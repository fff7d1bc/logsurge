package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFloodIntegrationProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping flood integration in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shell-style executable invocation not covered on windows")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "logsurge-test")
	for _, dir := range []string{"gocache", "gomodcache", "gopath", "gotmp", "telemetry"} {
		if err := os.MkdirAll(filepath.Join(tmp, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	build.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(tmp, "gocache"),
		"GOMODCACHE="+filepath.Join(tmp, "gomodcache"),
		"GOPATH="+filepath.Join(tmp, "gopath"),
		"GOTMPDIR="+filepath.Join(tmp, "gotmp"),
		"GOTELEMETRYDIR="+filepath.Join(tmp, "telemetry"),
		"GOENV=off",
		"GOTELEMETRY=off",
		"GOFLAGS=-modcacherw",
	)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	logDir := filepath.Join(tmp, "logs")
	cpuProfile := filepath.Join(tmp, "cpu.pprof")
	memProfile := filepath.Join(tmp, "mem.pprof")
	cmd := exec.Command(bin,
		"--format", "json",
		"--metadata", "source,line_end",
		"--output", "dir="+logDir,
		"--queue-records", "4096",
		"--queue-bytes", "4M",
		"--debug-cpuprofile", cpuProfile,
		"--debug-memprofile", memProfile,
		"--",
		bin, "__test-producer", "--records", "20000", "--line-bytes", "120",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("receiver failed: %v\nstderr=%s", err, stderr.String())
	}
	requireNonEmptyFile(t, filepath.Join(logDir, "current"))
	requireNonEmptyFile(t, cpuProfile)
	requireNonEmptyFile(t, memProfile)

	data, err := os.ReadFile(filepath.Join(logDir, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"line"`)) || !bytes.Contains(data, []byte(`"metadata"`)) {
		t.Fatalf("log output missing expected JSON fields: %.200q", data)
	}
	if !strings.Contains(string(data[:min(len(data), 500)]), `"source":"combined"`) {
		t.Fatalf("log output missing source metadata: %.500q", data)
	}
}

func requireNonEmptyFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatalf("%s is empty", path)
	}
}
