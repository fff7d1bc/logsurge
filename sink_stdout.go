package main

import (
	"bufio"
	"io"
)

type StdoutSink struct {
	w         *bufio.Writer
	formatter Formatter
}

func NewStdoutSink(out io.Writer, formatter Formatter) *StdoutSink {
	return &StdoutSink{
		w:         bufio.NewWriter(out),
		formatter: formatter,
	}
}

func (s *StdoutSink) WriteRecord(record Record) error {
	return s.formatter.Format(record, s.w)
}

func (s *StdoutSink) Flush() error {
	return s.w.Flush()
}

func (s *StdoutSink) Close() error {
	return s.Flush()
}
