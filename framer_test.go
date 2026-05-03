package main

import (
	"testing"
	"time"
)

func collectFramed(chunks []chunkResult, cfg FramerConfig) []Record {
	ch := make(chan chunkResult, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	q := NewQueue(100, 1024*1024, OverflowBlock)
	RunFramer(ch, q, cfg)
	var out []Record
	for {
		rec, ok := q.Pop()
		if !ok {
			break
		}
		out = append(out, rec)
	}
	return out
}

func TestFramerLFAndCRLF(t *testing.T) {
	records := collectFramed([]chunkResult{{data: []byte("a\nb\r\n")}}, FramerConfig{
		MaxFragmentBytes:     1024,
		PartialFlushInterval: 0,
	})
	if len(records) != 2 {
		t.Fatalf("len = %d", len(records))
	}
	if string(records[0].Line) != "a" || string(records[1].Line) != "b" {
		t.Fatalf("records = %#v", records)
	}
}

func TestFramerEOFPartial(t *testing.T) {
	records := collectFramed([]chunkResult{{data: []byte("partial")}}, FramerConfig{
		MaxFragmentBytes:     1024,
		PartialFlushInterval: 0,
	})
	if len(records) != 1 {
		t.Fatalf("len = %d", len(records))
	}
	if records[0].End != RecordEndEOF || string(records[0].Line) != "partial" {
		t.Fatalf("record = %#v", records[0])
	}
}

func TestFramerMaxBytes(t *testing.T) {
	records := collectFramed([]chunkResult{{data: []byte("abcdef\n")}}, FramerConfig{
		MaxFragmentBytes:     3,
		PartialFlushInterval: 0,
	})
	if len(records) != 2 {
		t.Fatalf("len = %d", len(records))
	}
	if records[0].End != RecordEndMaxBytes || string(records[0].Line) != "abc" {
		t.Fatalf("record0 = %#v", records[0])
	}
	if records[1].End != RecordEndNewline || string(records[1].Line) != "def" || !records[1].Continued {
		t.Fatalf("record1 = %#v", records[1])
	}
}

func TestFramerExactMaxBytesCanEndWithNewline(t *testing.T) {
	records := collectFramed([]chunkResult{{data: []byte("abc\n")}}, FramerConfig{
		MaxFragmentBytes:     3,
		PartialFlushInterval: 0,
	})
	if len(records) != 1 {
		t.Fatalf("len = %d", len(records))
	}
	if records[0].End != RecordEndNewline || string(records[0].Line) != "abc" || records[0].Continued {
		t.Fatalf("record = %#v", records[0])
	}
}

func TestFramerTimeoutPartial(t *testing.T) {
	ch := make(chan chunkResult, 1)
	q := NewQueue(100, 1024*1024, OverflowBlock)
	go RunFramer(ch, q, FramerConfig{
		MaxFragmentBytes:     1024,
		PartialFlushInterval: 20 * time.Millisecond,
	})
	ch <- chunkResult{data: []byte("progress")}
	time.Sleep(80 * time.Millisecond)
	ch <- chunkResult{data: []byte("done\n")}
	close(ch)
	time.Sleep(20 * time.Millisecond)
	var records []Record
	for {
		rec, ok := q.Pop()
		if !ok {
			break
		}
		records = append(records, rec)
	}
	if len(records) != 2 {
		t.Fatalf("len = %d records=%#v", len(records), records)
	}
	if records[0].End != RecordEndTimeout || string(records[0].Line) != "progress" {
		t.Fatalf("record0 = %#v", records[0])
	}
	if records[1].End != RecordEndNewline || !records[1].Continued || string(records[1].Line) != "done" {
		t.Fatalf("record1 = %#v", records[1])
	}
}
