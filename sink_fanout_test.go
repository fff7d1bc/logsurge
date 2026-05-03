package main

import (
	"errors"
	"testing"
	"time"
)

type recordingSink struct {
	writes   []string
	flushed  bool
	closed   bool
	writeErr error
	flushErr error
	closeErr error
}

func (s *recordingSink) WriteRecord(record Record) error {
	s.writes = append(s.writes, string(record.Line))
	return s.writeErr
}

func (s *recordingSink) Flush() error {
	s.flushed = true
	return s.flushErr
}

func (s *recordingSink) Close() error {
	s.closed = true
	return s.closeErr
}

func TestFanoutSinkWritesAllSinksInOrder(t *testing.T) {
	first := &recordingSink{}
	second := &recordingSink{}
	sink := fanoutSink{sinks: []Sink{first, second}}

	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if len(first.writes) != 1 || first.writes[0] != "x" {
		t.Fatalf("first writes = %#v", first.writes)
	}
	if len(second.writes) != 1 || second.writes[0] != "x" {
		t.Fatalf("second writes = %#v", second.writes)
	}
}

func TestFanoutSinkWriteStopsOnFirstError(t *testing.T) {
	wantErr := errors.New("write failed")
	first := &recordingSink{writeErr: wantErr}
	second := &recordingSink{}
	sink := fanoutSink{sinks: []Sink{first, second}}

	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
	if len(first.writes) != 1 {
		t.Fatalf("first writes = %#v", first.writes)
	}
	if len(second.writes) != 0 {
		t.Fatalf("second writes = %#v", second.writes)
	}
}

func TestFanoutSinkFlushAndCloseAllSinks(t *testing.T) {
	flushErr := errors.New("flush failed")
	closeErr := errors.New("close failed")
	first := &recordingSink{flushErr: flushErr, closeErr: closeErr}
	second := &recordingSink{}
	sink := fanoutSink{sinks: []Sink{first, second}}

	if err := sink.Flush(); !errors.Is(err, flushErr) {
		t.Fatalf("flush err = %v", err)
	}
	if !first.flushed || !second.flushed {
		t.Fatalf("flushed first=%v second=%v", first.flushed, second.flushed)
	}
	if err := sink.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("close err = %v", err)
	}
	if !first.closed || !second.closed {
		t.Fatalf("closed first=%v second=%v", first.closed, second.closed)
	}
}
