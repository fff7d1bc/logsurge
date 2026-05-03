package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

type JSONFormatter struct {
	MetadataFields []MetadataField
}

func (f JSONFormatter) Format(record Record, w io.Writer) error {
	// This formatter is deliberately hand-written instead of using
	// encoding/json structs/maps. JSON output is on the hot path for command
	// wrapping and HTTP forwarding; profiling showed the reflective encoder and
	// per-record metadata maps dominated allocations. Keep every value emitted
	// through appendJSONTime, appendJSONString*, or appendJSONValue so this stays
	// valid JSON rather than ad-hoc string concatenation.
	buf := make([]byte, 0, len(record.Line)+160+len(record.Metadata)*32+len(f.MetadataFields)*24)
	if record.End == RecordEndInternal {
		buf = appendJSONInternalRecord(buf, record)
	} else {
		buf = f.appendRecord(buf, record)
	}
	_, err := w.Write(buf)
	return err
}

func (f JSONFormatter) appendRecord(buf []byte, record Record) []byte {
	buf = append(buf, `{"time":`...)
	buf = appendJSONTime(buf, record.Time)
	buf = append(buf, `,"line":`...)
	buf = appendJSONStringBytes(buf, record.Line)
	if f.hasMetadata(record) {
		buf = append(buf, `,"metadata":{`...)
		buf = f.appendMetadata(buf, record)
		buf = append(buf, '}')
	}
	buf = append(buf, "}\n"...)
	return buf
}

func (f JSONFormatter) hasMetadata(record Record) bool {
	return len(record.Metadata) > 0 || len(f.MetadataFields) > 0
}

func (f JSONFormatter) appendMetadata(buf []byte, record Record) []byte {
	first := true
	for key, value := range record.Metadata {
		if f.metadataFieldOverridesKey(key) {
			continue
		}
		buf = appendJSONMetadataPair(buf, &first, key, value)
	}
	for _, field := range f.MetadataFields {
		switch field {
		case MetadataHostname:
			buf = appendJSONMetadataPair(buf, &first, "hostname", record.Metadata["hostname"])
		case MetadataSource:
			buf = appendJSONMetadataPair(buf, &first, "source", recordSource(record))
		case MetadataLineEnd:
			buf = appendJSONMetadataPair(buf, &first, "line_end", recordEndString(record.End))
		case MetadataContinued:
			buf = appendJSONMetadataPair(buf, &first, "continued", record.Continued)
		}
	}
	return buf
}

func (f JSONFormatter) metadataFieldOverridesKey(key string) bool {
	for _, field := range f.MetadataFields {
		if string(field) == key {
			return true
		}
	}
	return false
}

func appendJSONInternalRecord(buf []byte, record Record) []byte {
	buf = append(buf, `{"time":`...)
	buf = appendJSONTime(buf, record.Time)
	buf = append(buf, `,"source":"logsurge","event":`...)
	buf = appendJSONString(buf, record.InternalEvent)
	if record.Message != "" {
		buf = append(buf, `,"message":`...)
		buf = appendJSONString(buf, record.Message)
	}
	buf = append(buf, `,"records":`...)
	buf = strconv.AppendUint(buf, record.DroppedRecords, 10)
	buf = append(buf, `,"bytes":`...)
	buf = strconv.AppendUint(buf, record.DroppedBytes, 10)
	buf = append(buf, `,"reason":`...)
	buf = appendJSONString(buf, record.Reason)
	buf = append(buf, "}\n"...)
	return buf
}

func appendJSONMetadataPair(buf []byte, first *bool, key string, value any) []byte {
	if !*first {
		buf = append(buf, ',')
	}
	*first = false
	buf = appendJSONString(buf, key)
	buf = append(buf, ':')
	return appendJSONValue(buf, value)
}

func appendJSONValue(buf []byte, value any) []byte {
	// Custom metadata is validated at config/load time to this scalar set. The
	// fallback keeps this helper defensive for tests or future internal callers,
	// but normal log records avoid json.Marshal on the hot path.
	switch v := value.(type) {
	case nil:
		return append(buf, "null"...)
	case string:
		return appendJSONString(buf, v)
	case bool:
		return strconv.AppendBool(buf, v)
	case json.Number:
		if s := v.String(); json.Valid([]byte(s)) {
			return append(buf, s...)
		}
		return appendJSONString(buf, v.String())
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return appendJSONString(buf, strconv.FormatFloat(v, 'g', -1, 64))
		}
		return strconv.AppendFloat(buf, v, 'g', -1, 64)
	case int:
		return strconv.AppendInt(buf, int64(v), 10)
	case int64:
		return strconv.AppendInt(buf, v, 10)
	case uint64:
		return strconv.AppendUint(buf, v, 10)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return appendJSONString(buf, fmt.Sprint(v))
		}
		return append(buf, encoded...)
	}
}

func appendJSONTime(buf []byte, t time.Time) []byte {
	buf = append(buf, '"')
	buf = t.AppendFormat(buf, "2006-01-02T15:04:05,000000000-07:00")
	buf = append(buf, '"')
	return buf
}

func appendJSONString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '\\' && c != '"' {
				i++
				continue
			}
			buf = append(buf, s[start:i]...)
			switch c {
			case '\\', '"':
				buf = append(buf, '\\', c)
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				buf = append(buf, `\u00`...)
				buf = appendHexByte(buf, c)
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf = append(buf, s[start:i]...)
			buf = append(buf, `\ufffd`...)
			i++
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			buf = append(buf, s[start:i]...)
			if r == '\u2028' {
				buf = append(buf, `\u2028`...)
			} else {
				buf = append(buf, `\u2029`...)
			}
			i += size
			start = i
			continue
		}
		i += size
	}
	buf = append(buf, s[start:]...)
	buf = append(buf, '"')
	return buf
}

func appendJSONStringBytes(buf []byte, s []byte) []byte {
	// Match encoding/json string escaping for the cases log lines can contain:
	// quotes, backslashes, ASCII controls, valid UTF-8, invalid UTF-8, and the
	// U+2028/U+2029 separators that json.Encoder also escapes. This is the key
	// safety boundary for manual JSON output.
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '\\' && c != '"' {
				i++
				continue
			}
			buf = append(buf, s[start:i]...)
			switch c {
			case '\\', '"':
				buf = append(buf, '\\', c)
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				buf = append(buf, `\u00`...)
				buf = appendHexByte(buf, c)
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRune(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf = append(buf, s[start:i]...)
			buf = append(buf, `\ufffd`...)
			i++
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			buf = append(buf, s[start:i]...)
			if r == '\u2028' {
				buf = append(buf, `\u2028`...)
			} else {
				buf = append(buf, `\u2029`...)
			}
			i += size
			start = i
			continue
		}
		i += size
	}
	buf = append(buf, s[start:]...)
	buf = append(buf, '"')
	return buf
}

func appendHexByte(buf []byte, b byte) []byte {
	const hex = "0123456789abcdef"
	return append(buf, hex[b>>4], hex[b&0x0f])
}

func recordSource(record Record) string {
	if record.Source != "" {
		return record.Source
	}
	return "unknown"
}
