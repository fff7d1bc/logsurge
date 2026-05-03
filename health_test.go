package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHealthServer(t *testing.T) {
	stats := NewRuntimeStats()
	q := NewQueue(4, 1024, OverflowDropOldest)
	input := stats.RegisterInput("tcp", "tcp://127.0.0.1:5514", q)
	q.Push(RecordMeta{}, []byte("hello"))
	input.WrittenRecords.Add(1)

	server, err := startHealthServer("127.0.0.1:0", stats)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	base := "http://" + server.ln.Addr().String()

	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
	resp, err = http.Post(base+"/health", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("health post status = %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"logsurge_input_records_accepted_total",
		"logsurge_input_records_written_total",
		"logsurge_queue_records",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics missing %q: %s", want, text)
		}
	}
}

func TestHealthServerHealthIsLiveness(t *testing.T) {
	stats := NewRuntimeStats()
	stats.IncSinkErrors()
	server, err := startHealthServer("127.0.0.1:0", stats)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	base := "http://" + server.ln.Addr().String()
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
}

func TestHealthServerRejectsNonLoopback(t *testing.T) {
	if _, err := startHealthServer("0.0.0.0:0", NewRuntimeStats()); err == nil {
		t.Fatal("expected error")
	}
}

func TestMetricsEscapesLabelValues(t *testing.T) {
	stats := NewRuntimeStats()
	q := NewQueue(4, 1024, OverflowDropOldest)
	stats.RegisterInput("file", "quote\" slash\\ newline\n", q)

	server, err := startHealthServer("127.0.0.1:0", stats)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	resp, err := http.Get("http://" + server.ln.Addr().String() + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `source="quote\" slash\\ newline\n"`) {
		t.Fatalf("metrics did not escape labels: %s", data)
	}
}
