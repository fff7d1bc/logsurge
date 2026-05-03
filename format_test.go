package main

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func fixedTime() time.Time {
	return time.Date(2026, 5, 2, 11, 49, 53, 951493000, time.FixedZone("x", 2*60*60))
}

func TestPlainFormatter(t *testing.T) {
	var buf bytes.Buffer
	err := PlainFormatter{}.Format(Record{
		Time: fixedTime(),
		Line: []byte("hello"),
		End:  RecordEndNewline,
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	want := "2026-05-02T11:49:53,951493000+02:00 hello\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPlainFormatterPartialEnd(t *testing.T) {
	var buf bytes.Buffer
	err := PlainFormatter{}.Format(Record{
		Time:      fixedTime(),
		Line:      []byte("done"),
		End:       RecordEndNewline,
		Continued: true,
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[partial-end] done") {
		t.Fatalf("unexpected output %q", buf.String())
	}
}

func TestJSONFormatter(t *testing.T) {
	var buf bytes.Buffer
	err := JSONFormatter{}.Format(Record{
		Time: fixedTime(),
		Line: []byte("hello"),
		End:  RecordEndNewline,
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["time"] == nil || got["line"] != "hello" {
		t.Fatalf("unexpected json %#v", got)
	}
}

func TestJSONFormatterMetadata(t *testing.T) {
	var buf bytes.Buffer
	err := JSONFormatter{MetadataFields: []MetadataField{MetadataHostname, MetadataSource, MetadataLineEnd, MetadataContinued}}.Format(Record{
		Time:   fixedTime(),
		Line:   []byte("hello"),
		End:    RecordEndNewline,
		Source: "combined",
		Metadata: map[string]any{
			"hostname": "builder",
		},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	metadata := got["metadata"].(map[string]any)
	if got["line"] != "hello" || got["source"] != nil || got["line_end"] != nil || got["continued"] != nil {
		t.Fatalf("unexpected top-level json %#v", got)
	}
	if metadata["hostname"] != "builder" || metadata["source"] != "combined" || metadata["line_end"] != "newline" || metadata["continued"] != false {
		t.Fatalf("unexpected json %#v", got)
	}
}

func TestJSONFormatterCustomMetadata(t *testing.T) {
	var buf bytes.Buffer
	err := JSONFormatter{}.Format(Record{
		Time: fixedTime(),
		Line: []byte("hello"),
		End:  RecordEndNewline,
		Metadata: map[string]any{
			"terraform_run": "retry",
			"attempt":       2,
		},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	metadata := got["metadata"].(map[string]any)
	if metadata["terraform_run"] != "retry" || metadata["attempt"] != float64(2) {
		t.Fatalf("unexpected json %#v", got)
	}
}

func TestJSONFormatterMetadataScalarTypes(t *testing.T) {
	var buf bytes.Buffer
	err := JSONFormatter{}.Format(Record{
		Time: fixedTime(),
		Line: []byte("hello"),
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
	var got map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.UseNumber()
	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}
	metadata := got["metadata"].(map[string]any)
	if metadata["string"] != "value" || metadata["number"] != json.Number("42.5") || metadata["bool"] != true || metadata["null"] != nil {
		t.Fatalf("unexpected json %#v", got)
	}
}

func TestJSONFormatterEscapesLineBytes(t *testing.T) {
	var buf bytes.Buffer
	line := []byte("quote\" slash\\ newline\n tab\t utf8指定 \xff \xe2\x80\xa8")
	err := JSONFormatter{}.Format(Record{
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
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `quote\"`) || !strings.Contains(buf.String(), `slash\\`) || !strings.Contains(buf.String(), `newline\n`) {
		t.Fatalf("line was not escaped as expected: %q", buf.String())
	}
	if got["line"] != "quote\" slash\\ newline\n tab\t utf8指定 \ufffd \u2028" {
		t.Fatalf("unexpected json %#v raw=%q", got, buf.String())
	}
}

func TestJSONFormatterEscapesJSONShapedLineBytes(t *testing.T) {
	var buf bytes.Buffer
	line := []byte(`{"outer":"{\"inner\":\"quote \\\" slash \\\\ newline \\n\"}","array":["{\"x\":1}"]}`)
	err := JSONFormatter{}.Format(Record{
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
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["line"] != string(line) {
		t.Fatalf("line changed got=%q want=%q raw=%q", got["line"], string(line), buf.String())
	}
}

func TestJSONFormatterMetadataKeepsInvalidNumbersValidJSON(t *testing.T) {
	var buf bytes.Buffer
	err := JSONFormatter{}.Format(Record{
		Time: fixedTime(),
		Line: []byte("hello"),
		End:  RecordEndNewline,
		Metadata: map[string]any{
			"bad_number": json.Number("01"),
			"nan":        math.NaN(),
			"inf":        math.Inf(1),
		},
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(bytes.TrimSpace(buf.Bytes())) {
		t.Fatalf("invalid json %q", buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	metadata := got["metadata"].(map[string]any)
	if metadata["bad_number"] != "01" || metadata["nan"] != "NaN" || metadata["inf"] != "+Inf" {
		t.Fatalf("metadata = %#v raw=%q", metadata, buf.String())
	}
}

func TestDropSummaryFormatting(t *testing.T) {
	var buf bytes.Buffer
	err := JSONFormatter{}.Format(Record{
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
	if !strings.Contains(buf.String(), `"event":"dropped"`) {
		t.Fatalf("unexpected json %q", buf.String())
	}
}
