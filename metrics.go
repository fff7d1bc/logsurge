package main

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

type RuntimeStats struct {
	inputsMu sync.Mutex
	inputs   []*InputStats

	sinkErrors atomic.Uint64
}

type InputStats struct {
	Kind   string
	Source string
	Queue  *Queue

	AcceptedRecords atomic.Uint64
	AcceptedBytes   atomic.Uint64
	WrittenRecords  atomic.Uint64
	WrittenBytes    atomic.Uint64
	DroppedRecords  atomic.Uint64
	DroppedBytes    atomic.Uint64
	SourceErrors    atomic.Uint64
	TCPAccepted     atomic.Uint64
	TCPClosed       atomic.Uint64
	TCPActive       atomic.Int64
	UDPDatagrams    atomic.Uint64
}

func NewRuntimeStats() *RuntimeStats {
	return &RuntimeStats{}
}

func (s *RuntimeStats) RegisterInput(kind string, source string, q *Queue) *InputStats {
	if s == nil {
		return nil
	}
	stats := &InputStats{Kind: kind, Source: source, Queue: q}
	s.inputsMu.Lock()
	s.inputs = append(s.inputs, stats)
	s.inputsMu.Unlock()
	if q != nil {
		q.SetStats(stats)
	}
	return stats
}

func (s *RuntimeStats) IncSinkErrors() {
	if s != nil {
		s.sinkErrors.Add(1)
	}
}

func (s *RuntimeStats) Inputs() []*InputStats {
	if s == nil {
		return nil
	}
	s.inputsMu.Lock()
	defer s.inputsMu.Unlock()
	return append([]*InputStats(nil), s.inputs...)
}

func (s *RuntimeStats) WritePrometheus(w io.Writer) {
	if s == nil {
		return
	}
	writeMetric(w, "logsurge_sink_errors_total", nil, s.sinkErrors.Load())
	for i, input := range s.Inputs() {
		labels := map[string]string{
			"input":  fmt.Sprintf("%d", i),
			"kind":   input.Kind,
			"source": input.Source,
		}
		writeMetric(w, "logsurge_input_records_accepted_total", labels, input.AcceptedRecords.Load())
		writeMetric(w, "logsurge_input_bytes_accepted_total", labels, input.AcceptedBytes.Load())
		writeMetric(w, "logsurge_input_records_written_total", labels, input.WrittenRecords.Load())
		writeMetric(w, "logsurge_input_bytes_written_total", labels, input.WrittenBytes.Load())
		writeMetric(w, "logsurge_input_records_dropped_total", labels, input.DroppedRecords.Load())
		writeMetric(w, "logsurge_input_bytes_dropped_total", labels, input.DroppedBytes.Load())
		writeMetric(w, "logsurge_input_source_errors_total", labels, input.SourceErrors.Load())
		if input.Queue != nil {
			snap := input.Queue.Snapshot()
			writeMetric(w, "logsurge_queue_records", labels, uint64(snap.Records))
			writeMetric(w, "logsurge_queue_bytes", labels, uint64(snap.Bytes))
			writeMetric(w, "logsurge_queue_records_max", labels, uint64(snap.MaxRecords))
			writeMetric(w, "logsurge_queue_bytes_max", labels, uint64(snap.MaxBytes))
		}
		if input.Kind == "tcp" {
			writeMetric(w, "logsurge_tcp_connections_accepted_total", labels, input.TCPAccepted.Load())
			writeMetric(w, "logsurge_tcp_connections_closed_total", labels, input.TCPClosed.Load())
			active := input.TCPActive.Load()
			if active < 0 {
				active = 0
			}
			writeMetric(w, "logsurge_tcp_connections_active", labels, uint64(active))
		}
		if input.Kind == "udp" {
			writeMetric(w, "logsurge_udp_datagrams_total", labels, input.UDPDatagrams.Load())
		}
	}
}

func writeMetric(w io.Writer, name string, labels map[string]string, value uint64) {
	if len(labels) == 0 {
		fmt.Fprintf(w, "%s %d\n", name, value)
		return
	}
	fmt.Fprintf(w, "%s{", name)
	first := true
	for _, key := range []string{"input", "kind", "source"} {
		value, ok := labels[key]
		if !ok {
			continue
		}
		if !first {
			fmt.Fprint(w, ",")
		}
		first = false
		fmt.Fprintf(w, `%s="`, key)
		writePrometheusLabelValue(w, value)
		fmt.Fprint(w, `"`)
	}
	fmt.Fprintf(w, "} %d\n", value)
}

func writePrometheusLabelValue(w io.Writer, value string) {
	for _, r := range value {
		switch r {
		case '\\':
			fmt.Fprint(w, `\\`)
		case '"':
			fmt.Fprint(w, `\"`)
		case '\n':
			fmt.Fprint(w, `\n`)
		default:
			fmt.Fprint(w, string(r))
		}
	}
}
