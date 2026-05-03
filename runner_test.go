package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testRunnerConfig(script string) Config {
	cfg := DefaultConfig()
	cfg.Command = []string{"/bin/sh", "-c", script}
	cfg.PartialFlushInterval = 50 * time.Millisecond
	cfg.FlushInterval = 10 * time.Millisecond
	cfg.PostExitDrainTimeout = 200 * time.Millisecond
	cfg.QueueRecords = 1024
	cfg.QueueBytes = 1024 * 1024
	return cfg
}

func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires /bin/sh")
	}
}

func TestRunnerMergedOutput(t *testing.T) {
	requireShell(t)
	var out, err bytes.Buffer
	code := Runner{
		Config: testRunnerConfig("printf 'out\\n'; printf 'err\\n' >&2"),
		Stdout: &out,
		Stderr: &err,
	}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, err.String())
	}
	got := out.String()
	if !strings.Contains(got, " out\n") || !strings.Contains(got, " err\n") {
		t.Fatalf("output = %q", got)
	}
}

func TestRunnerExitCode(t *testing.T) {
	requireShell(t)
	for i := 0; i < 20; i++ {
		var out, err bytes.Buffer
		code := Runner{
			Config: testRunnerConfig("exit 7"),
			Stdout: &out,
			Stderr: &err,
		}.Run()
		if code != 7 {
			t.Fatalf("iteration %d: code = %d stderr=%q", i, code, err.String())
		}
	}
}

func TestRunnerPartialBeforeExit(t *testing.T) {
	requireShell(t)
	var out, err bytes.Buffer
	code := Runner{
		Config: testRunnerConfig("printf progress; sleep 0.15; printf done"),
		Stdout: &out,
		Stderr: &err,
	}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, err.String())
	}
	got := out.String()
	if !strings.Contains(got, "[partial] progress") || !strings.Contains(got, "[partial-eof] done") {
		t.Fatalf("output = %q", got)
	}
}

func TestRunnerPostExitDrainTimeout(t *testing.T) {
	requireShell(t)
	var out, err bytes.Buffer
	cfg := testRunnerConfig("(sleep 2) & printf 'done\\n'")
	cfg.PostExitDrainTimeout = 100 * time.Millisecond
	start := time.Now()
	code := Runner{Config: cfg, Stdout: &out, Stderr: &err}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, err.String())
	}
	if time.Since(start) > time.Second {
		t.Fatalf("runner took too long")
	}
	if !strings.Contains(out.String(), " done\n") {
		t.Fatalf("output = %q", out.String())
	}
	if !strings.Contains(out.String(), "post-exit drain timeout") {
		t.Fatalf("missing timeout diagnostic: %q", out.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type failOnWriteBuffer struct {
	bytes.Buffer
}

func (w *failOnWriteBuffer) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if err != nil {
		return n, err
	}
	return n, errors.New("write failed after capture")
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestRunnerSinkFailure(t *testing.T) {
	requireShell(t)
	var err bytes.Buffer
	cfg := testRunnerConfig("trap '' TERM; printf 'x\\n'; sleep 10")
	cfg.TerminationTimeout = 100 * time.Millisecond
	start := time.Now()
	code := Runner{
		Config: cfg,
		Stdout: failWriter{},
		Stderr: &err,
	}.Run()
	if code == 0 {
		t.Fatal("expected nonzero")
	}
	if time.Since(start) > time.Second {
		t.Fatal("sink failure did not force-kill promptly")
	}
}

func TestRunnerDirectoryOutput(t *testing.T) {
	requireShell(t)
	dir := t.TempDir()
	var err bytes.Buffer
	cfg := testRunnerConfig("printf 'dir-output\\n'")
	cfg.Output = OutputDir
	cfg.OutputTarget = dir
	code := Runner{
		Config: cfg,
		Stderr: &err,
	}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, err.String())
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "current"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), " dir-output\n") {
		t.Fatalf("current = %q", string(data))
	}
}

func TestRunnerStdinSource(t *testing.T) {
	var out, err bytes.Buffer
	cfg := DefaultConfig()
	cfg.Source = SourceStdin
	cfg.PartialFlushInterval = 0
	cfg.FlushInterval = 10 * time.Millisecond
	code := Runner{
		Config: cfg,
		Stdin:  strings.NewReader("from-stdin\npartial"),
		Stdout: &out,
		Stderr: &err,
	}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, err.String())
	}
	got := out.String()
	if !strings.Contains(got, " from-stdin\n") || !strings.Contains(got, "[partial-eof] partial") {
		t.Fatalf("output = %q", got)
	}
}

func TestRunnerStdinSourceMetadata(t *testing.T) {
	var out, err bytes.Buffer
	cfg := DefaultConfig()
	cfg.Source = SourceStdin
	cfg.Format = FormatJSON
	cfg.MetadataFields = []MetadataField{MetadataSource}
	cfg.PartialFlushInterval = 0
	cfg.FlushInterval = 10 * time.Millisecond
	code := Runner{
		Config: cfg,
		Stdin:  strings.NewReader("line\n"),
		Stdout: &out,
		Stderr: &err,
	}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, err.String())
	}
	if !strings.Contains(out.String(), `"source":"stdin"`) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunnerStdinSourceReadError(t *testing.T) {
	var out, err bytes.Buffer
	cfg := DefaultConfig()
	cfg.Source = SourceStdin
	cfg.FlushInterval = 10 * time.Millisecond
	code := Runner{
		Config: cfg,
		Stdin:  errReader{},
		Stdout: &out,
		Stderr: &err,
	}.Run()
	if code == 0 {
		t.Fatal("expected nonzero")
	}
	if !strings.Contains(err.String(), "read failed") {
		t.Fatalf("stderr = %q", err.String())
	}
}

func TestRunnerSinkFailurePrefersSinkError(t *testing.T) {
	var err bytes.Buffer
	cfg := DefaultConfig()
	cfg.Source = SourceStdin
	cfg.FlushInterval = 10 * time.Millisecond
	code := Runner{
		Config: cfg,
		Stdin:  strings.NewReader("line\n"),
		Stdout: failWriter{},
		Stderr: &err,
	}.Run()
	if code == 0 {
		t.Fatal("expected nonzero")
	}
	if !strings.Contains(err.String(), "write failed") || strings.Contains(err.String(), "read failed") {
		t.Fatalf("stderr = %q", err.String())
	}
}

func TestCombineSourceAndWriterCountsSourceError(t *testing.T) {
	sourceDone := make(chan error, 1)
	writerDone := make(chan error, 1)
	stats := &InputStats{}
	sourceDone <- errors.New("read failed")
	writerDone <- nil
	err := <-combineSourceAndWriter(sourceDone, writerDone, stats)
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("err = %v", err)
	}
	if stats.SourceErrors.Load() != 1 {
		t.Fatalf("source errors = %d", stats.SourceErrors.Load())
	}
}

func TestCombineSourceAndWriterPrefersWriterError(t *testing.T) {
	sourceDone := make(chan error, 1)
	writerDone := make(chan error, 1)
	stats := &InputStats{}
	sourceDone <- errors.New("read failed")
	writerDone <- errors.New("write failed")
	err := <-combineSourceAndWriter(sourceDone, writerDone, stats)
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("err = %v", err)
	}
	if stats.SourceErrors.Load() != 0 {
		t.Fatalf("source errors = %d", stats.SourceErrors.Load())
	}
}

func TestRunnerJSONKeepsUTF8AndStripsANSI(t *testing.T) {
	requireShell(t)
	var out, err bytes.Buffer
	cfg := testRunnerConfig("printf '%s\\n' '/tmp/指定材料.webp' '/tmp/供电1.webp'; printf '\\033[31mred\\033[0m\\n'; printf '%s\\n' '/tmp/next'")
	cfg.Format = FormatJSON
	code := Runner{
		Config: cfg,
		Stdout: &out,
		Stderr: &err,
	}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, err.String())
	}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var obj struct {
			Line string `json:"line"`
		}
		if unmarshalErr := json.Unmarshal([]byte(line), &obj); unmarshalErr != nil {
			t.Fatalf("invalid json line %q: %v", line, unmarshalErr)
		}
		got = append(got, obj.Line)
	}
	want := []string{"/tmp/指定材料.webp", "/tmp/供电1.webp", "red", "/tmp/next"}
	if len(got) != len(want) {
		t.Fatalf("lines = %#v, want %#v; raw=%q", got, want, out.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q; all=%#v raw=%q", i, got[i], want[i], got, out.String())
		}
		if got[i] == "" {
			t.Fatalf("line %d became empty; all=%#v raw=%q", i, got, out.String())
		}
	}
}

func TestRunnerConfigModeMultipleFilesJSON(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")
	if err := os.WriteFile(a, []byte("a1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.ConfigMode = true
	cfg.Format = FormatJSON
	cfg.MetadataFields = []MetadataField{MetadataSource}
	cfg.FlushInterval = 10 * time.Millisecond
	cfg.Inputs = []InputConfig{
		configModeTestInput(a, "alpha"),
		configModeTestInput(b, "beta"),
	}
	var err bytes.Buffer
	out := &failOnWriteBuffer{}
	code := Runner{Config: cfg, Stdout: out, Stderr: &err}.Run()
	if code == 0 {
		t.Fatal("expected sink failure to stop daemon-mode runner")
	}
	got := out.String()
	if !strings.Contains(got, `"source":"alpha"`) || !strings.Contains(got, `"source":"beta"`) {
		t.Fatalf("output = %q stderr=%q", got, err.String())
	}
}

func TestRunnerConfigModePlainPrefixesSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.ConfigMode = true
	cfg.Format = FormatPlain
	cfg.FlushInterval = 10 * time.Millisecond
	cfg.Inputs = []InputConfig{configModeTestInput(path, "app")}
	var err bytes.Buffer
	out := &failOnWriteBuffer{}
	code := Runner{Config: cfg, Stdout: out, Stderr: &err}.Run()
	if code == 0 {
		t.Fatal("expected sink failure to stop daemon-mode runner")
	}
	if !strings.Contains(out.String(), " [app] line\n") {
		t.Fatalf("output = %q", out.String())
	}
}

func configModeTestInput(path string, source string) InputConfig {
	cfg := DefaultConfig()
	return InputConfig{
		Path:                 path,
		Source:               source,
		QueueRecords:         cfg.QueueRecords,
		QueueBytes:           cfg.QueueBytes,
		Overflow:             cfg.Overflow,
		MaxFragmentBytes:     cfg.MaxFragmentBytes,
		PartialFlushInterval: 0,
		FileStart:            FileStartBeginning,
		FilePollInterval:     10 * time.Millisecond,
	}
}

func TestRunnerClosesSinkOnStartFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix-style missing path")
	}
	dir := t.TempDir()
	var err bytes.Buffer
	cfg := DefaultConfig()
	cfg.Output = OutputDir
	cfg.OutputTarget = dir
	cfg.Command = []string{"/definitely/missing/logsurge-test-command"}
	code := Runner{
		Config: cfg,
		Stderr: &err,
	}.Run()
	if code == 0 {
		t.Fatal("expected nonzero")
	}
	sink, openErr := NewDirectorySink(dir, PlainFormatter{}, DirectorySinkConfig{
		MaxBytes: 1000,
		MaxFiles: 10,
	})
	if openErr != nil {
		t.Fatalf("sink was not released after start failure: %v", openErr)
	}
	if closeErr := sink.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
}
