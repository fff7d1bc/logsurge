package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestJournalctlArgs(t *testing.T) {
	args := journalctlArgs(InputConfig{
		Directory:    "/var/log/journal",
		JournalStart: JournalStartEnd,
		CursorFile:   "/var/lib/logsurge/journal.cursor",
	})
	got := strings.Join(args, " ")
	for _, want := range []string{
		"--directory=/var/log/journal",
		"--follow",
		"--lines=0",
		"--output=json",
		"--cursor-file=/var/lib/logsurge/journal.cursor",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}

	args = journalctlArgs(InputConfig{Directory: "/var/log/journal", JournalStart: JournalStartAll})
	got = strings.Join(args, " ")
	if !strings.Contains(got, "--no-tail") || strings.Contains(got, "--lines=0") {
		t.Fatalf("all args = %#v", args)
	}
}

func TestParseJournalRecord(t *testing.T) {
	rec, err := parseJournalRecord([]byte(`{"MESSAGE":"hello","__REALTIME_TIMESTAMP":"2000000","__CURSOR":"cursor-a","_SYSTEMD_UNIT":"ssh.service","SYSLOG_IDENTIFIER":"sshd","PRIORITY":"6","_PID":"123","_TRANSPORT":"stdout","_BOOT_ID":"boot-a"}`), "journald", map[string]any{"environment": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if string(rec.Line) != "hello" || rec.Source != "journald" || !rec.Time.Equal(time.Unix(2, 0)) {
		t.Fatalf("record = %#v", rec)
	}
	for key, want := range map[string]any{
		"environment":        "test",
		"journal_cursor":     "cursor-a",
		"journal_unit":       "ssh.service",
		"journal_identifier": "sshd",
		"journal_priority":   "6",
		"journal_pid":        "123",
		"journal_transport":  "stdout",
		"journal_boot_id":    "boot-a",
	} {
		if rec.Metadata[key] != want {
			t.Fatalf("metadata[%s] = %#v want %#v in %#v", key, rec.Metadata[key], want, rec.Metadata)
		}
	}
}

func TestParseJournalRecordMissingMessage(t *testing.T) {
	rec, err := parseJournalRecord([]byte(`{"__REALTIME_TIMESTAMP":"2000000"}`), "journald", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rec.Line), "without MESSAGE") || rec.Metadata["journal_message_omitted"] != true {
		t.Fatalf("record = %#v", rec)
	}
}

func TestRunnerConfigModeJournalInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "journalctl")
	entry := map[string]string{
		"MESSAGE":              "journal-line",
		"__REALTIME_TIMESTAMP": "2000000",
		"__CURSOR":             "cursor-a",
		"_SYSTEMD_UNIT":        "app.service",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' '"+string(data)+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := journalctlCommand
	journalctlCommand = script
	t.Cleanup(func() { journalctlCommand = old })

	cfg := DefaultConfig()
	cfg.ConfigMode = true
	cfg.Format = FormatJSON
	cfg.MetadataFields = []MetadataField{MetadataSource}
	cfg.FlushInterval = 10 * time.Millisecond
	cfg.Inputs = []InputConfig{{
		Kind:                 InputKindJournal,
		Directory:            "/var/log/journal",
		JournalStart:         JournalStartEnd,
		Source:               "journald",
		QueueRecords:         1024,
		QueueBytes:           1024 * 1024,
		Overflow:             OverflowDropOldest,
		MaxFragmentBytes:     64 * 1024,
		PartialFlushInterval: time.Second,
		ANSI:                 ANSIStrip,
		FilePollInterval:     10 * time.Millisecond,
	}}
	var out, stderr strings.Builder
	code := Runner{Config: cfg, Stdout: &out, Stderr: &stderr}.Run()
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, stderr.String())
	}
	got := out.String()
	if !strings.Contains(got, `"line":"journal-line"`) || !strings.Contains(got, `"source":"journald"`) || !strings.Contains(got, `"journal_unit":"app.service"`) {
		t.Fatalf("output = %q stderr=%q", got, stderr.String())
	}
}
