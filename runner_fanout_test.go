package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunnerFanoutStdoutAndHTTP(t *testing.T) {
	requireShell(t)

	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := testRunnerConfig("printf 'x\\n'")
	cfg.Format = FormatJSON
	cfg.MetadataFields = []MetadataField{MetadataSource, MetadataLineEnd}
	cfg.Outputs = []OutputConfig{
		{Kind: OutputStdout},
		{Kind: OutputHTTP, Target: server.URL, HTTPBatchRecords: 1, HTTPTimeout: time.Second, HTTPRetriesSet: true},
	}

	var out, stderr bytes.Buffer
	code := Runner{Config: cfg, Stdout: &out, Stderr: &stderr}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"line":"x"`) || !strings.Contains(out.String(), `"source":"combined"`) {
		t.Fatalf("stdout = %q", out.String())
	}
	if len(bodies) != 1 {
		t.Fatalf("bodies = %#v", bodies)
	}
	if !strings.Contains(bodies[0], `"_msg":"x"`) || !strings.Contains(bodies[0], `"source":"combined"`) {
		t.Fatalf("http body = %q", bodies[0])
	}
}
