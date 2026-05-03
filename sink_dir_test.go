package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirectorySinkWritesCurrent(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewDirectorySink(dir, PlainFormatter{}, DirectorySinkConfig{
		MaxBytes: 1000,
		MaxFiles: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), " hello\n") {
		t.Fatalf("current = %q", string(data))
	}
}

func TestDirectorySinkRotatesAndRetains(t *testing.T) {
	dir := t.TempDir()
	for name := range map[string]string{
		"app.log":                             "unrelated",
		"@20260503T151459.815047695Z.s":       "legacy",
		"20260503T151459.815047695Z.tmp":      "temporary",
		"20260503T151459.815047695Z~001.tmp":  "temporary collision",
		"20260503T151459.815047695Zextra.log": "unrelated",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sink, err := NewDirectorySink(dir, PlainFormatter{}, DirectorySinkConfig{
		MaxBytes: 60,
		MaxFiles: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err := sink.WriteRecord(Record{Time: time.Unix(int64(i), 0), End: RecordEndNewline, Line: []byte("line-line-line")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	rotated, err := rotatedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rotated) == 0 {
		t.Fatal("expected rotated files")
	}
	if len(rotated) > 2 {
		t.Fatalf("rotated files = %v", rotated)
	}
	for _, name := range rotated {
		if strings.HasPrefix(name, "@") || !isRotationName(name) {
			t.Fatalf("unexpected rotated filename %q", name)
		}
	}
	for _, name := range []string{
		"app.log",
		"@20260503T151459.815047695Z.s",
		"20260503T151459.815047695Z.tmp",
		"20260503T151459.815047695Z~001.tmp",
		"20260503T151459.815047695Zextra.log",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("unrelated file %q was not preserved: %v", name, err)
		}
	}
}

func TestDirectorySinkRotationNamePattern(t *testing.T) {
	tests := map[string]bool{
		"20260503T151459.815047695Z.log":      true,
		"20260503T151459.815047695Z~001.log":  true,
		"@20260503T151459.815047695Z.s":       false,
		"app.log":                             false,
		"20260503T151459.815047695Z.tmp":      false,
		"20260503T151459.815047695Zextra.log": false,
	}
	for name, want := range tests {
		if got := isRotationName(name); got != want {
			t.Fatalf("isRotationName(%q) = %v, want %v", name, got, want)
		}
	}
}
