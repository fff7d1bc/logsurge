package main

import (
	"sync"
	"time"
)

func RunWriter(q *Queue, sink Sink, flushInterval time.Duration, ansi ansiMode, onError func(error)) error {
	return RunWriterWithStats(q, sink, flushInterval, ansi, onError, nil)
}

func RunWriterWithStats(q *Queue, sink Sink, flushInterval time.Duration, ansi ansiMode, onError func(error), stats *InputStats) error {
	var once sync.Once
	var stripper *ansiStripper
	if ansi == ANSIStrip {
		stripper = &ansiStripper{}
	}
	report := func(err error) {
		if err != nil && onError != nil {
			once.Do(func() { onError(err) })
		}
	}
	var ticker *time.Ticker
	var tick <-chan time.Time
	if flushInterval > 0 {
		ticker = time.NewTicker(flushInterval)
		defer ticker.Stop()
		tick = ticker.C
	}
	for {
		select {
		case <-tick:
			if err := sink.Flush(); err != nil {
				report(err)
				q.Close()
				return err
			}
		case _, open := <-q.Wake():
			if err := drainQueue(q, sink, stripper, stats); err != nil {
				report(err)
				q.Close()
				return err
			}
			if !open {
				// Wake closes only after the framer has closed the queue. Drain
				// first, then emit final drop diagnostics, then close the sink so
				// buffered HTTP/dir batches are not abandoned.
				if err := emitDropSummary(q, sink); err != nil {
					report(err)
					return err
				}
				if err := sink.Close(); err != nil {
					report(err)
					return err
				}
				return nil
			}
		}
	}
}

func drainQueue(q *Queue, sink Sink, stripper *ansiStripper, stats *InputStats) error {
	// Emit drop summaries before and during draining so long floods produce
	// visible, coalesced diagnostics without one diagnostic per dropped record.
	if err := emitDropSummary(q, sink); err != nil {
		return err
	}
	for {
		rec, ok := q.Pop()
		if !ok {
			break
		}
		if stripper != nil && rec.End != RecordEndInternal {
			// Strip after dequeue so the ingest path stays timestamp-and-queue
			// only. ANSI cleanup cost belongs on the writer side.
			rec.Line = stripper.Strip(rec.Line)
		}
		if err := sink.WriteRecord(rec); err != nil {
			return err
		}
		if stats != nil && rec.End != RecordEndInternal {
			stats.WrittenRecords.Add(1)
			stats.WrittenBytes.Add(uint64(len(rec.Line)))
		}
		if err := emitDropSummary(q, sink); err != nil {
			return err
		}
	}
	return nil
}

func emitDropSummary(q *Queue, sink Sink) error {
	drops := q.TakeDrops()
	if drops.Records == 0 {
		return nil
	}
	// Drop summaries are internal records: they bypass ANSI stripping and are
	// coalesced so floods do not create one diagnostic per lost record.
	return sink.WriteRecord(Record{
		Time:           time.Now(),
		End:            RecordEndInternal,
		InternalEvent:  "dropped",
		DroppedRecords: drops.Records,
		DroppedBytes:   drops.Bytes,
		Reason:         dropReason(drops.Policy),
	})
}

func dropReason(policy OverflowPolicy) string {
	switch policy {
	case OverflowDropOldest:
		return "queue_full_drop_oldest"
	case OverflowDropNewest:
		return "queue_full_drop_newest"
	default:
		return "queue_full"
	}
}
