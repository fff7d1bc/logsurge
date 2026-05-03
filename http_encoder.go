package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
)

type HTTPRecordEncoder interface {
	Encode(record Record, w io.Writer) error
	ContentType() string
}

type JSONLineHTTPEncoder struct {
	MetadataFields []MetadataField
}

func (e JSONLineHTTPEncoder) ContentType() string {
	return "application/x-ndjson"
}

func (e JSONLineHTTPEncoder) Encode(record Record, w io.Writer) error {
	buf := make([]byte, 0, len(record.Line)+160+len(record.Metadata)*32+len(e.MetadataFields)*24)
	buf = append(buf, `{"_time":`...)
	buf = appendJSONTime(buf, record.Time)
	if record.End == RecordEndInternal {
		msg := record.Message
		if record.InternalEvent == "dropped" {
			msg = "dropped records because output was slower than input"
		}
		buf = append(buf, `,"_msg":`...)
		buf = appendJSONString(buf, msg)
		buf = append(buf, `,"event":`...)
		buf = appendJSONString(buf, record.InternalEvent)
		buf = append(buf, `,"records":`...)
		buf = strconv.AppendUint(buf, record.DroppedRecords, 10)
		buf = append(buf, `,"bytes":`...)
		buf = strconv.AppendUint(buf, record.DroppedBytes, 10)
		buf = append(buf, `,"reason":`...)
		buf = appendJSONString(buf, record.Reason)
	} else {
		buf = append(buf, `,"_msg":`...)
		buf = appendJSONStringBytes(buf, record.Line)
		buf = e.appendMetadata(buf, record)
	}
	buf = append(buf, "}\n"...)
	_, err := w.Write(buf)
	return err
}

func (e JSONLineHTTPEncoder) appendMetadata(buf []byte, record Record) []byte {
	for key, value := range record.Metadata {
		if e.metadataFieldOverridesKey(key) {
			continue
		}
		buf = append(buf, ',')
		buf = appendJSONString(buf, key)
		buf = append(buf, ':')
		buf = appendJSONValue(buf, value)
	}
	for _, field := range e.MetadataFields {
		switch field {
		case MetadataHostname:
			buf = appendHTTPMetadataPair(buf, "hostname", record.Metadata["hostname"])
		case MetadataSource:
			buf = appendHTTPMetadataPair(buf, "source", recordSource(record))
		case MetadataLineEnd:
			buf = appendHTTPMetadataPair(buf, "line_end", recordEndString(record.End))
		case MetadataContinued:
			buf = appendHTTPMetadataPair(buf, "continued", record.Continued)
		}
	}
	return buf
}

func (e JSONLineHTTPEncoder) metadataFieldOverridesKey(key string) bool {
	for _, field := range e.MetadataFields {
		if string(field) == key {
			return true
		}
	}
	return false
}

type GELFHTTPEncoder struct {
	Host           string
	MetadataFields []MetadataField
}

func NewGELFHTTPEncoder(fields []MetadataField) GELFHTTPEncoder {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return GELFHTTPEncoder{Host: host, MetadataFields: fields}
}

func (e GELFHTTPEncoder) ContentType() string {
	return "application/x-ndjson"
}

func (e GELFHTTPEncoder) Encode(record Record, w io.Writer) error {
	buf := make([]byte, 0, len(record.Line)+180+len(record.Metadata)*36+len(e.MetadataFields)*28)
	buf = append(buf, `{"version":"1.1","host":`...)
	buf = appendJSONString(buf, e.host())
	buf = append(buf, `,"timestamp":`...)
	buf = strconv.AppendFloat(buf, float64(record.Time.UnixNano())/1e9, 'g', -1, 64)
	if record.End == RecordEndInternal {
		msg := record.Message
		if record.InternalEvent == "dropped" {
			msg = "dropped records because output was slower than input"
		}
		buf = append(buf, `,"short_message":`...)
		buf = appendJSONString(buf, msg)
		buf = append(buf, `,"_event":`...)
		buf = appendJSONString(buf, record.InternalEvent)
		buf = append(buf, `,"_records":`...)
		buf = strconv.AppendUint(buf, record.DroppedRecords, 10)
		buf = append(buf, `,"_bytes":`...)
		buf = strconv.AppendUint(buf, record.DroppedBytes, 10)
		buf = append(buf, `,"_reason":`...)
		buf = appendJSONString(buf, record.Reason)
	} else {
		buf = append(buf, `,"short_message":`...)
		buf = appendJSONStringBytes(buf, record.Line)
		buf = e.appendMetadata(buf, record)
	}
	buf = append(buf, "}\n"...)
	_, err := w.Write(buf)
	return err
}

func (e GELFHTTPEncoder) host() string {
	if e.Host != "" {
		return e.Host
	}
	return "unknown"
}

func (e GELFHTTPEncoder) appendMetadata(buf []byte, record Record) []byte {
	for key, value := range record.Metadata {
		if e.metadataFieldOverridesKey(key) {
			continue
		}
		buf = append(buf, ',')
		buf = appendJSONString(buf, "_"+key)
		buf = append(buf, ':')
		buf = appendJSONValue(buf, value)
	}
	for _, field := range e.MetadataFields {
		switch field {
		case MetadataHostname:
			if record.Metadata["hostname"] != nil {
				buf = appendHTTPMetadataPair(buf, "_hostname", record.Metadata["hostname"])
			} else {
				buf = appendHTTPMetadataPair(buf, "_hostname", e.host())
			}
		case MetadataSource:
			buf = appendHTTPMetadataPair(buf, "_source", recordSource(record))
		case MetadataLineEnd:
			buf = appendHTTPMetadataPair(buf, "_line_end", recordEndString(record.End))
		case MetadataContinued:
			buf = appendHTTPMetadataPair(buf, "_continued", record.Continued)
		}
	}
	return buf
}

func (e GELFHTTPEncoder) metadataFieldOverridesKey(key string) bool {
	for _, field := range e.MetadataFields {
		if string(field) == key {
			return true
		}
	}
	return false
}

func appendHTTPMetadataPair(buf []byte, key string, value any) []byte {
	buf = append(buf, ',')
	buf = appendJSONString(buf, key)
	buf = append(buf, ':')
	return appendJSONValue(buf, value)
}

func newHTTPRecordEncoder(format HTTPFormat, fields []MetadataField) (HTTPRecordEncoder, error) {
	switch format {
	case HTTPFormatJSONLine:
		return JSONLineHTTPEncoder{MetadataFields: fields}, nil
	case HTTPFormatGELF:
		return NewGELFHTTPEncoder(fields), nil
	default:
		return nil, fmt.Errorf("unsupported HTTP format %q", format)
	}
}
