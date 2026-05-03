package main

import (
	"fmt"
	"io"
)

type Formatter interface {
	Format(record Record, w io.Writer) error
}

type PlainFormatter struct {
	IncludeSource bool
}

func (f PlainFormatter) Format(record Record, w io.Writer) error {
	ts := timestampString(record.Time)
	if record.End == RecordEndInternal {
		if record.InternalEvent != "dropped" {
			_, err := fmt.Fprintf(w, "%s [logsurge] %s\n", ts, record.Message)
			return err
		}
		which := "records"
		switch record.Reason {
		case "queue_full_drop_oldest":
			which = "oldest records"
		case "queue_full_drop_newest":
			which = "newest records"
		}
		_, err := fmt.Fprintf(w, "%s [logsurge] dropped %d %s / %d bytes because output was slower than input\n", ts, record.DroppedRecords, which, record.DroppedBytes)
		return err
	}
	marker := plainMarker(record)
	source := ""
	if f.IncludeSource {
		source = "[" + recordSource(record) + "] "
	}
	if marker == "" {
		_, err := fmt.Fprintf(w, "%s %s%s\n", ts, source, record.Line)
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s%s %s\n", ts, source, marker, record.Line)
	return err
}

func plainMarker(record Record) string {
	switch record.End {
	case RecordEndTimeout:
		return "[partial]"
	case RecordEndEOF:
		return "[partial-eof]"
	case RecordEndMaxBytes:
		return "[partial-max]"
	case RecordEndNewline:
		if record.Continued {
			return "[partial-end]"
		}
	}
	return ""
}
