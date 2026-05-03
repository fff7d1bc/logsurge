package main

type fanoutSink struct {
	sinks []Sink
}

func (s fanoutSink) WriteRecord(record Record) error {
	for _, sink := range s.sinks {
		if err := sink.WriteRecord(record); err != nil {
			return err
		}
	}
	return nil
}

func (s fanoutSink) Flush() error {
	var firstErr error
	for _, sink := range s.sinks {
		if err := sink.Flush(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s fanoutSink) Close() error {
	var firstErr error
	for _, sink := range s.sinks {
		if err := sink.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
