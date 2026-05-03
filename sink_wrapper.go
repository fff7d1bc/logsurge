package main

import "sync"

type lockedSink struct {
	mu   sync.Mutex
	sink Sink
}

func (s *lockedSink) WriteRecord(record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sink.WriteRecord(record)
}

func (s *lockedSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sink.Flush()
}

func (s *lockedSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sink.Close()
}

type noCloseSink struct {
	sink Sink
}

// Daemon mode starts one writer per input but has one shared sink. Input
// writers should flush shared batches, but only runConfigMode may close the
// sink after every input has finished.
func (s noCloseSink) WriteRecord(record Record) error { return s.sink.WriteRecord(record) }
func (s noCloseSink) Flush() error                    { return s.sink.Flush() }
func (s noCloseSink) Close() error                    { return nil }

func sinkForWriter(sink Sink, closeSink bool) Sink {
	if closeSink {
		return sink
	}
	return noCloseSink{sink: sink}
}

type closeFunc func()

func (f closeFunc) Close() error {
	f()
	return nil
}
