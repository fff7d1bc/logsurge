package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memorySink struct {
	mu      sync.Mutex
	records []Record
	flushed int
	closed  bool
}

func (s *memorySink) WriteRecord(record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.Line != nil {
		record.Line = append([]byte(nil), record.Line...)
	}
	s.records = append(s.records, record)
	return nil
}

func (s *memorySink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushed++
	return nil
}

func (s *memorySink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func TestWriterEmitsDropSummary(t *testing.T) {
	q := NewQueue(1, 1024, OverflowDropNewest)
	if !q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("one")) {
		t.Fatal("first push dropped")
	}
	if q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("two")) {
		t.Fatal("second push unexpectedly accepted")
	}
	q.Close()
	sink := &memorySink{}
	if err := RunWriter(q, sink, 0, ANSIKeep, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) < 2 {
		t.Fatalf("records = %#v", sink.records)
	}
	if sink.records[0].End != RecordEndInternal || sink.records[0].InternalEvent != "dropped" {
		t.Fatalf("missing drop summary first: %#v", sink.records)
	}
	if sink.records[0].DroppedRecords != 1 || sink.records[0].DroppedBytes != 3 {
		t.Fatalf("drop summary = %#v", sink.records[0])
	}
	if sink.records[0].Reason != "queue_full_drop_newest" {
		t.Fatalf("reason = %q", sink.records[0].Reason)
	}
}

type slowSink struct {
	memorySink
	delay time.Duration
}

func (s *slowSink) WriteRecord(record Record) error {
	time.Sleep(s.delay)
	return s.memorySink.WriteRecord(record)
}

func TestWriterHandlesSlowSink(t *testing.T) {
	q := NewQueue(4, 1024, OverflowDropOldest)
	sink := &slowSink{delay: 10 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		done <- RunWriter(q, sink, 5*time.Millisecond, ANSIKeep, nil)
	}()
	for i := 0; i < 20; i++ {
		q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte(strings.Repeat("x", 8)))
	}
	q.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer hung with slow sink")
	}
	foundDrop := false
	for _, rec := range sink.records {
		if rec.End == RecordEndInternal && rec.InternalEvent == "dropped" {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Fatalf("missing drop diagnostic: %#v", sink.records)
	}
}

type failingSink struct{}

func (failingSink) WriteRecord(Record) error { return errors.New("write failed") }
func (failingSink) Flush() error             { return nil }
func (failingSink) Close() error             { return nil }

func TestWriterCallsOnErrorOnce(t *testing.T) {
	q := NewQueue(4, 1024, OverflowBlock)
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("line"))
	q.Close()
	calls := 0
	err := RunWriter(q, failingSink{}, 0, ANSIKeep, func(error) { calls++ })
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestWriterStripsANSI(t *testing.T) {
	q := NewQueue(4, 1024, OverflowBlock)
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("\x1b[31mred\x1b[0m"))
	q.Close()
	sink := &memorySink{}
	if err := RunWriter(q, sink, 0, ANSIStrip, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) != 1 || string(sink.records[0].Line) != "red" {
		t.Fatalf("records = %#v", sink.records)
	}
}

func TestWriterKeepsANSI(t *testing.T) {
	q := NewQueue(4, 1024, OverflowBlock)
	line := []byte("\x1b[31mred\x1b[0m")
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, line)
	q.Close()
	sink := &memorySink{}
	if err := RunWriter(q, sink, 0, ANSIKeep, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) != 1 || string(sink.records[0].Line) != string(line) {
		t.Fatalf("records = %#v", sink.records)
	}
}

func TestWriterStripsANSISplitAcrossRecords(t *testing.T) {
	q := NewQueue(4, 1024, OverflowBlock)
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndTimeout}, []byte("a\x1b[3"))
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("1mb"))
	q.Close()
	sink := &memorySink{}
	if err := RunWriter(q, sink, 0, ANSIStrip, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) != 2 {
		t.Fatalf("records = %#v", sink.records)
	}
	if string(sink.records[0].Line) != "a" || string(sink.records[1].Line) != "b" {
		t.Fatalf("records = %#v", sink.records)
	}
}

func TestWriterStripsANSIButKeepsUTF8Records(t *testing.T) {
	q := NewQueue(8, 4096, OverflowBlock)
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("/tmp/指定材料.webp"))
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("/tmp/next"))
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("\x1b[31mred\x1b[0m"))
	q.Close()
	sink := &memorySink{}
	if err := RunWriter(q, sink, 0, ANSIStrip, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/指定材料.webp", "/tmp/next", "red"}
	if len(sink.records) != len(want) {
		t.Fatalf("records = %#v", sink.records)
	}
	for i, rec := range sink.records {
		if string(rec.Line) != want[i] {
			t.Fatalf("record %d line = %q, want %q; records=%#v", i, rec.Line, want[i], sink.records)
		}
		if len(rec.Line) == 0 {
			t.Fatalf("record %d became empty: %#v", i, sink.records)
		}
	}
}

func TestWriterDoesNotStripInternalRecord(t *testing.T) {
	q := NewQueue(4, 1024, OverflowBlock)
	q.PushInternal(Record{
		Time:          time.Now(),
		End:           RecordEndInternal,
		InternalEvent: "test",
		Line:          []byte("\x1b[31minternal\x1b[0m"),
	})
	q.Close()
	sink := &memorySink{}
	if err := RunWriter(q, sink, 0, ANSIStrip, nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) != 1 || string(sink.records[0].Line) != "\x1b[31minternal\x1b[0m" {
		t.Fatalf("records = %#v", sink.records)
	}
}
