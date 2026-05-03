package main

import (
	"bytes"
	"testing"
)

func TestStartChunkReaderSplitsAndReleasesBuffers(t *testing.T) {
	chunks := StartChunkReader(bytes.NewBufferString("abcdefghijkl"), 3)
	var got string
	for res := range chunks {
		if res.err != nil {
			t.Fatalf("unexpected read error: %v", res.err)
		}
		got += string(res.data)
		releaseChunk(res)
	}
	if got != "abcdefghijkl" {
		t.Fatalf("got %q", got)
	}
}

func TestStartChunkReaderDeliversBytesBeforeError(t *testing.T) {
	chunks := StartChunkReader(errorAfterDataReader{}, 16)
	res, ok := <-chunks
	if !ok {
		t.Fatal("missing data chunk")
	}
	if string(res.data) != "before-error" {
		t.Fatalf("data = %q", res.data)
	}
	releaseChunk(res)
	res, ok = <-chunks
	if !ok {
		t.Fatal("missing error chunk")
	}
	if res.err == nil {
		t.Fatal("missing read error")
	}
	if _, ok := <-chunks; ok {
		t.Fatal("extra chunk after error")
	}
}

type errorAfterDataReader struct{}

func (r errorAfterDataReader) Read(p []byte) (int, error) {
	copy(p, "before-error")
	return len("before-error"), errReadAfterData
}

type readAfterDataError string

func (e readAfterDataError) Error() string { return string(e) }

const errReadAfterData = readAfterDataError("read failed after data")
