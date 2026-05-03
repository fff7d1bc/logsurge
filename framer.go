package main

import (
	"bytes"
	"time"
)

type FramerConfig struct {
	MaxFragmentBytes     int
	PartialFlushInterval time.Duration
	Metadata             map[string]any
	Source               string
}

func RunFramer(chunks <-chan chunkResult, q *Queue, cfg FramerConfig) error {
	return runFramer(chunks, q, cfg, true)
}

func RunFramerNoClose(chunks <-chan chunkResult, q *Queue, cfg FramerConfig) error {
	// Network TCP input can have several active connections feeding one source
	// queue. Each connection gets its own framer state, and the listener closes
	// the queue only after every connection framer has returned.
	return runFramer(chunks, q, cfg, false)
}

func runFramer(chunks <-chan chunkResult, q *Queue, cfg FramerConfig, closeQueue bool) error {
	if closeQueue {
		defer q.Close()
	}
	// This is intentionally not bufio.Scanner. Scanner has token limits and a
	// line-oriented blocking shape that fights partial-timeout flushing and
	// max-fragment splitting for very large log records.
	f := &framer{
		q:                    q,
		maxFragmentBytes:     cfg.MaxFragmentBytes,
		partialFlushInterval: cfg.PartialFlushInterval,
		metadata:             cfg.Metadata,
		source:               cfg.Source,
	}
	return f.run(chunks)
}

type framer struct {
	q                    *Queue
	buf                  []byte
	maxFragmentBytes     int
	partialFlushInterval time.Duration
	continued            bool
	metadata             map[string]any
	source               string
}

func (f *framer) run(chunks <-chan chunkResult) error {
	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerC = nil
	}
	resetTimer := func() {
		if f.partialFlushInterval == 0 || len(f.buf) == 0 {
			stopTimer()
			return
		}
		if timer == nil {
			timer = time.NewTimer(f.partialFlushInterval)
			timerC = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(f.partialFlushInterval)
		timerC = timer.C
	}
	defer stopTimer()
	for {
		select {
		case res, ok := <-chunks:
			if !ok {
				f.emitEOF()
				return nil
			}
			if res.err != nil {
				f.emitEOF()
				return res.err
			}
			f.consume(res.data)
			releaseChunk(res)
			resetTimer()
		case <-timerC:
			// The read goroutine may be blocked waiting for more pipe bytes, so
			// idle partial flushing has to live in this select loop rather than
			// in a line-oriented blocking reader.
			f.emit(RecordEndTimeout, f.buf)
			f.buf = f.buf[:0]
			f.continued = true
			resetTimer()
		}
	}
}

func releaseChunk(res chunkResult) {
	if res.release == nil {
		return
	}
	res.release <- res.buf
}

func (f *framer) consume(data []byte) {
	for len(data) > 0 {
		if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
			f.buf = append(f.buf, data[:idx]...)
			// Use >, not >=. An exactly max-sized line followed by a newline is
			// still a complete line; splitting it would create an empty
			// continuation record after the newline.
			for len(f.buf) > f.maxFragmentBytes {
				f.emit(RecordEndMaxBytes, f.buf[:f.maxFragmentBytes])
				copy(f.buf, f.buf[f.maxFragmentBytes:])
				f.buf = f.buf[:len(f.buf)-f.maxFragmentBytes]
				f.continued = true
			}
			line := trimCR(f.buf)
			f.emit(RecordEndNewline, line)
			f.buf = f.buf[:0]
			f.continued = false
			data = data[idx+1:]
			continue
		}
		f.buf = append(f.buf, data...)
		// Without a newline we must cap memory even if the child writes a huge
		// stream. Leaving exactly maxFragmentBytes buffered lets a following
		// newline finish as a normal record.
		for len(f.buf) > f.maxFragmentBytes {
			f.emit(RecordEndMaxBytes, f.buf[:f.maxFragmentBytes])
			copy(f.buf, f.buf[f.maxFragmentBytes:])
			f.buf = f.buf[:len(f.buf)-f.maxFragmentBytes]
			f.continued = true
		}
		return
	}
}

func (f *framer) emitEOF() {
	if len(f.buf) == 0 {
		return
	}
	f.emit(RecordEndEOF, f.buf)
	f.buf = f.buf[:0]
}

func (f *framer) emit(end RecordEnd, line []byte) {
	f.q.Push(RecordMeta{
		Time:      time.Now(),
		End:       end,
		Source:    f.source,
		Continued: f.continued,
		Metadata:  f.metadata,
	}, line)
}

func trimCR(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\r' {
		return line[:len(line)-1]
	}
	return line
}
