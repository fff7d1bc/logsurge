package main

import (
	"net"
	"testing"
	"time"
)

func TestNetworkSourceTCP(t *testing.T) {
	q := NewQueue(16, 1024, OverflowDropOldest)
	stats := &InputStats{Kind: "tcp", Source: "tcp://127.0.0.1:0", Queue: q}
	q.SetStats(stats)
	source := newNetworkSource(InputConfig{
		Kind:                 InputKindTCP,
		Listen:               "127.0.0.1:0",
		Source:               "tcp-test",
		MaxConnections:       2,
		MaxFragmentBytes:     1024,
		PartialFlushInterval: 0,
	}, nil, stats)
	done, err := source.Start(q)
	if err != nil {
		t.Fatal(err)
	}
	addr := source.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("one\npartial")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	waitForRecords(t, q, 2)
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	first, ok := q.Pop()
	if !ok || string(first.Line) != "one" || first.Source != "tcp-test" || first.End != RecordEndNewline {
		t.Fatalf("first = %#v ok=%v", first, ok)
	}
	second, ok := q.Pop()
	if !ok || string(second.Line) != "partial" || second.End != RecordEndEOF {
		t.Fatalf("second = %#v ok=%v", second, ok)
	}
	if stats.TCPAccepted.Load() != 1 || stats.TCPClosed.Load() != 1 {
		t.Fatalf("tcp stats accepted=%d closed=%d", stats.TCPAccepted.Load(), stats.TCPClosed.Load())
	}
}

func TestNetworkSourceTCPMaxConnections(t *testing.T) {
	q := NewQueue(16, 1024, OverflowDropOldest)
	stats := &InputStats{Kind: "tcp", Source: "tcp://127.0.0.1:0", Queue: q}
	q.SetStats(stats)
	source := newNetworkSource(InputConfig{
		Kind:                 InputKindTCP,
		Listen:               "127.0.0.1:0",
		Source:               "tcp-test",
		MaxConnections:       1,
		MaxFragmentBytes:     1024,
		PartialFlushInterval: time.Second,
	}, nil, stats)
	done, err := source.Start(q)
	if err != nil {
		t.Fatal(err)
	}
	addr := source.listener.Addr().String()
	first, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	waitForActiveTCP(t, stats, 1)
	second, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Close()
	time.Sleep(50 * time.Millisecond)
	if stats.TCPAccepted.Load() != 1 {
		t.Fatalf("accepted = %d", stats.TCPAccepted.Load())
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNetworkSourceUDP(t *testing.T) {
	q := NewQueue(16, 1024, OverflowDropOldest)
	stats := &InputStats{Kind: "udp", Source: "udp://127.0.0.1:0", Queue: q}
	q.SetStats(stats)
	source := newNetworkSource(InputConfig{
		Kind:             InputKindUDP,
		Listen:           "127.0.0.1:0",
		Source:           "udp-test",
		MaxFragmentBytes: 3,
	}, nil, stats)
	done, err := source.Start(q)
	if err != nil {
		t.Fatal(err)
	}
	addr := source.packet.LocalAddr().String()
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	waitForRecords(t, q, 3)
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	first, ok := q.Pop()
	if !ok || string(first.Line) != "one" || first.Source != "udp-test" {
		t.Fatalf("first = %#v ok=%v", first, ok)
	}
	second, ok := q.Pop()
	if !ok || string(second.Line) != "abc" || second.End != RecordEndMaxBytes {
		t.Fatalf("second = %#v ok=%v", second, ok)
	}
	third, ok := q.Pop()
	if !ok || string(third.Line) != "def" || third.End != RecordEndNewline || !third.Continued {
		t.Fatalf("third = %#v ok=%v", third, ok)
	}
	if stats.UDPDatagrams.Load() != 2 {
		t.Fatalf("udp datagrams = %d", stats.UDPDatagrams.Load())
	}
}

func waitForRecords(t *testing.T, q *Queue, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if q.Snapshot().Records >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d records; snapshot=%#v", want, q.Snapshot())
}

func waitForActiveTCP(t *testing.T, stats *InputStats, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stats.TCPActive.Load() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for active tcp=%d; got %d", want, stats.TCPActive.Load())
}
