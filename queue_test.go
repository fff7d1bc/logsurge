package main

import (
	"testing"
	"time"
)

func TestQueueDropsOldestByRecordLimit(t *testing.T) {
	q := NewQueue(2, 1024, OverflowDropOldest)
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("one"))
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("two"))
	if !q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("three")) {
		t.Fatal("newest record dropped")
	}
	drops := q.TakeDrops()
	if drops.Records != 1 || drops.Bytes != 3 || drops.Policy != OverflowDropOldest {
		t.Fatalf("drops = %#v", drops)
	}
	rec, ok := q.Pop()
	if !ok || string(rec.Line) != "two" {
		t.Fatalf("first remaining = %#v ok=%v", rec, ok)
	}
	rec, ok = q.Pop()
	if !ok || string(rec.Line) != "three" {
		t.Fatalf("second remaining = %#v ok=%v", rec, ok)
	}
}

func TestQueueStatsAcceptedDroppedAndSnapshot(t *testing.T) {
	q := NewQueue(1, 4, OverflowDropNewest)
	stats := &InputStats{}
	q.SetStats(stats)
	if !q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("one")) {
		t.Fatal("first push dropped")
	}
	if q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("two")) {
		t.Fatal("second push accepted")
	}
	snapshot := q.Snapshot()
	if snapshot.Records != 1 || snapshot.Bytes != 3 || snapshot.MaxRecords != 1 || snapshot.MaxBytes != 4 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if stats.AcceptedRecords.Load() != 1 || stats.AcceptedBytes.Load() != 3 {
		t.Fatalf("accepted stats records=%d bytes=%d", stats.AcceptedRecords.Load(), stats.AcceptedBytes.Load())
	}
	if stats.DroppedRecords.Load() != 1 || stats.DroppedBytes.Load() != 3 {
		t.Fatalf("drop stats records=%d bytes=%d", stats.DroppedRecords.Load(), stats.DroppedBytes.Load())
	}
}

func TestQueueDropsOldestByByteLimit(t *testing.T) {
	q := NewQueue(10, 6, OverflowDropOldest)
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("aa"))
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("bb"))
	q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("cc"))
	if !q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("dddd")) {
		t.Fatal("newest record dropped")
	}
	drops := q.TakeDrops()
	if drops.Records != 2 || drops.Bytes != 4 || drops.Policy != OverflowDropOldest {
		t.Fatalf("drops = %#v", drops)
	}
	rec, ok := q.Pop()
	if !ok || string(rec.Line) != "cc" {
		t.Fatalf("first remaining = %#v ok=%v", rec, ok)
	}
	rec, ok = q.Pop()
	if !ok || string(rec.Line) != "dddd" {
		t.Fatalf("second remaining = %#v ok=%v", rec, ok)
	}
}

func TestQueueDropOldestRejectsOversizedRecord(t *testing.T) {
	q := NewQueue(10, 3, OverflowDropOldest)
	if q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("xxxx")) {
		t.Fatal("oversized record accepted")
	}
	drops := q.TakeDrops()
	if drops.Records != 1 || drops.Bytes != 4 || drops.Policy != OverflowDropOldest {
		t.Fatalf("drops = %#v", drops)
	}
}

func TestQueueDropsNewestByRecordLimit(t *testing.T) {
	q := NewQueue(1, 1024, OverflowDropNewest)
	if !q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("one")) {
		t.Fatal("first push dropped")
	}
	if q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("two")) {
		t.Fatal("second push accepted")
	}
	drops := q.TakeDrops()
	if drops.Records != 1 || drops.Bytes != 3 || drops.Policy != OverflowDropNewest {
		t.Fatalf("drops = %#v", drops)
	}
}

func TestQueueDropsNewestByByteLimit(t *testing.T) {
	q := NewQueue(10, 3, OverflowDropNewest)
	if !q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("abc")) {
		t.Fatal("first push dropped")
	}
	if q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("d")) {
		t.Fatal("second push accepted")
	}
	drops := q.TakeDrops()
	if drops.Records != 1 || drops.Bytes != 1 || drops.Policy != OverflowDropNewest {
		t.Fatalf("drops = %#v", drops)
	}
}

func TestQueueBlockWaitsForCapacity(t *testing.T) {
	q := NewQueue(1, 1024, OverflowBlock)
	if !q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("one")) {
		t.Fatal("first push dropped")
	}
	done := make(chan bool, 1)
	go func() {
		done <- q.Push(RecordMeta{Time: time.Now(), End: RecordEndNewline}, []byte("two"))
	}()
	select {
	case <-done:
		t.Fatal("push completed before capacity")
	case <-time.After(50 * time.Millisecond):
	}
	if _, ok := q.Pop(); !ok {
		t.Fatal("pop failed")
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("blocked push failed")
		}
	case <-time.After(time.Second):
		t.Fatal("push did not unblock")
	}
}
