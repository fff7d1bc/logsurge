package main

import "time"

type RecordEnd uint8

const (
	RecordEndNewline RecordEnd = iota
	RecordEndTimeout
	RecordEndEOF
	RecordEndMaxBytes
	RecordEndInternal
)

type Record struct {
	Time           time.Time
	Line           []byte
	Seq            uint64
	End            RecordEnd
	Source         string
	Continued      bool
	InternalEvent  string
	Message        string
	DroppedRecords uint64
	DroppedBytes   uint64
	Reason         string
	Metadata       map[string]any
}

type RecordMeta struct {
	Time      time.Time
	End       RecordEnd
	Source    string
	Continued bool
	Metadata  map[string]any
}

func timestampString(t time.Time) string {
	return t.Format("2006-01-02T15:04:05,000000000-07:00")
}

func recordEndString(end RecordEnd) string {
	switch end {
	case RecordEndNewline:
		return "newline"
	case RecordEndTimeout:
		return "timeout"
	case RecordEndEOF:
		return "eof"
	case RecordEndMaxBytes:
		return "max_bytes"
	case RecordEndInternal:
		return "internal"
	default:
		return "unknown"
	}
}
