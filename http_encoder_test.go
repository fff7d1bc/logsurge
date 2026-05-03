package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func decodeOneJSONLine(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &obj); err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestJSONLineHTTPEncoderRecord(t *testing.T) {
	var buf bytes.Buffer
	enc := JSONLineHTTPEncoder{MetadataFields: []MetadataField{MetadataSource, MetadataLineEnd, MetadataContinued, MetadataHostname}}
	err := enc.Encode(Record{
		Time:      fixedTime(),
		Line:      []byte("hello"),
		End:       RecordEndNewline,
		Source:    "combined",
		Continued: true,
		Metadata: map[string]any{
			"hostname":      "builder",
			"terraform_run": "retry",
		},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	obj := decodeOneJSONLine(t, buf.Bytes())
	if obj["_time"] != timestampString(fixedTime()) || obj["_msg"] != "hello" || obj["source"] != "combined" || obj["line_end"] != "newline" || obj["continued"] != true || obj["hostname"] != "builder" || obj["terraform_run"] != "retry" {
		t.Fatalf("obj = %#v", obj)
	}
}

func TestJSONLineHTTPEncoderDropDiagnostic(t *testing.T) {
	var buf bytes.Buffer
	err := (JSONLineHTTPEncoder{}).Encode(Record{
		Time:           fixedTime(),
		End:            RecordEndInternal,
		InternalEvent:  "dropped",
		DroppedRecords: 2,
		DroppedBytes:   3,
		Reason:         "queue_full_drop_oldest",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	obj := decodeOneJSONLine(t, buf.Bytes())
	if obj["event"] != "dropped" || obj["records"] != float64(2) || obj["bytes"] != float64(3) || obj["reason"] != "queue_full_drop_oldest" {
		t.Fatalf("obj = %#v", obj)
	}
}

func TestJSONLineHTTPEncoderMetadataScalarTypesAndEscaping(t *testing.T) {
	var buf bytes.Buffer
	err := (JSONLineHTTPEncoder{}).Encode(Record{
		Time: fixedTime(),
		Line: []byte("quote\" slash\\ newline\n utf8指定 \xff \xe2\x80\xa8"),
		End:  RecordEndNewline,
		Metadata: map[string]any{
			"string": "value",
			"number": json.Number("42.5"),
			"bool":   true,
			"null":   nil,
		},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(bytes.TrimSpace(buf.Bytes())) {
		t.Fatalf("invalid json %q", buf.String())
	}
	obj := decodeOneJSONLine(t, buf.Bytes())
	if obj["_msg"] != "quote\" slash\\ newline\n utf8指定 \ufffd \u2028" || obj["string"] != "value" || obj["number"] != float64(42.5) || obj["bool"] != true || obj["null"] != nil {
		t.Fatalf("obj = %#v raw=%q", obj, buf.String())
	}
}

func TestHTTPEncodersPreserveJSONShapedMessagesAsStrings(t *testing.T) {
	line := []byte(`{"outer":"{\"inner\":\"quote \\\" slash \\\\ newline \\n\"}"}`)
	for name, enc := range map[string]HTTPRecordEncoder{
		"jsonline": JSONLineHTTPEncoder{},
		"gelf":     GELFHTTPEncoder{Host: "host-a"},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			err := enc.Encode(Record{
				Time: fixedTime(),
				Line: line,
				End:  RecordEndNewline,
			}, &buf)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(bytes.TrimSpace(buf.Bytes())) {
				t.Fatalf("invalid json %q", buf.String())
			}
			obj := decodeOneJSONLine(t, buf.Bytes())
			key := "_msg"
			if name == "gelf" {
				key = "short_message"
			}
			if obj[key] != string(line) {
				t.Fatalf("%s changed got=%q want=%q raw=%q", key, obj[key], string(line), buf.String())
			}
		})
	}
}

func TestGELFHTTPEncoderRecord(t *testing.T) {
	var buf bytes.Buffer
	enc := GELFHTTPEncoder{Host: "host-a", MetadataFields: []MetadataField{MetadataSource, MetadataLineEnd, MetadataContinued, MetadataHostname}}
	err := enc.Encode(Record{
		Time:      time.Unix(2, 500_000_000),
		Line:      []byte("hello"),
		End:       RecordEndNewline,
		Source:    "combined",
		Continued: true,
		Metadata: map[string]any{
			"hostname":      "builder",
			"terraform_run": "retry",
		},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	obj := decodeOneJSONLine(t, buf.Bytes())
	if obj["version"] != "1.1" || obj["host"] != "host-a" || obj["short_message"] != "hello" || obj["timestamp"] != 2.5 || obj["_source"] != "combined" || obj["_line_end"] != "newline" || obj["_continued"] != true || obj["_hostname"] != "builder" || obj["_terraform_run"] != "retry" {
		t.Fatalf("obj = %#v", obj)
	}
}

func TestGELFHTTPEncoderMetadataScalarTypesAndEscaping(t *testing.T) {
	var buf bytes.Buffer
	err := (GELFHTTPEncoder{Host: "host-a"}).Encode(Record{
		Time: time.Unix(2, 500_000_000),
		Line: []byte("quote\" slash\\ newline\n utf8指定 \xff \xe2\x80\xa8"),
		End:  RecordEndNewline,
		Metadata: map[string]any{
			"string": "value",
			"number": json.Number("42.5"),
			"bool":   true,
			"null":   nil,
		},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(bytes.TrimSpace(buf.Bytes())) {
		t.Fatalf("invalid json %q", buf.String())
	}
	obj := decodeOneJSONLine(t, buf.Bytes())
	if obj["short_message"] != "quote\" slash\\ newline\n utf8指定 \ufffd \u2028" || obj["_string"] != "value" || obj["_number"] != float64(42.5) || obj["_bool"] != true || obj["_null"] != nil {
		t.Fatalf("obj = %#v raw=%q", obj, buf.String())
	}
}

func TestGELFHTTPEncoderFallbackHostAndDropDiagnostic(t *testing.T) {
	var buf bytes.Buffer
	err := (GELFHTTPEncoder{}).Encode(Record{
		Time:           fixedTime(),
		End:            RecordEndInternal,
		InternalEvent:  "dropped",
		DroppedRecords: 2,
		DroppedBytes:   3,
		Reason:         "queue_full_drop_oldest",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	obj := decodeOneJSONLine(t, buf.Bytes())
	if obj["host"] != "unknown" || obj["_event"] != "dropped" || obj["_records"] != float64(2) || obj["_bytes"] != float64(3) || obj["_reason"] != "queue_full_drop_oldest" {
		t.Fatalf("obj = %#v", obj)
	}
}
