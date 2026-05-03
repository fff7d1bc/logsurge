package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigFileData(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
format = "json"
metadata = ["line_end"]
ansi = "keep"
flush_interval = "50ms"

[output]
kind = "dir"
target = "/tmp/logsurge"
dir_max_bytes = "2M"
dir_max_files = 3

[queue]
records = 12
bytes = "1M"
overflow = "drop-newest"
max_fragment_bytes = "4K"

[file]
start = "beginning"
poll_interval = "25ms"
partial_flush_interval = "250ms"

[[inputs]]
path = "/var/log/app.log"
source = "app"
ansi = "strip"

[[inputs]]
path = "/var/log/other.log"
queue_records = 24
queue_bytes = "2M"
`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ConfigMode || cfg.Source != SourceFile {
		t.Fatalf("mode=%v source=%q", cfg.ConfigMode, cfg.Source)
	}
	if cfg.Output != OutputDir || cfg.OutputTarget != "/tmp/logsurge" {
		t.Fatalf("output=%q target=%q", cfg.Output, cfg.OutputTarget)
	}
	if cfg.ANSI != ANSIKeep {
		t.Fatalf("ansi=%q", cfg.ANSI)
	}
	if cfg.QueueRecords != 12 || cfg.QueueBytes != 1024*1024 || cfg.Overflow != OverflowDropNewest {
		t.Fatalf("queue=%d/%d/%s", cfg.QueueRecords, cfg.QueueBytes, cfg.Overflow)
	}
	if cfg.FileStart != FileStartBeginning || cfg.FilePollInterval != 25*time.Millisecond {
		t.Fatalf("file defaults=%s/%s", cfg.FileStart, cfg.FilePollInterval)
	}
	if len(cfg.Inputs) != 2 {
		t.Fatalf("inputs=%d", len(cfg.Inputs))
	}
	if cfg.Inputs[0].Source != "app" || cfg.Inputs[0].QueueRecords != 12 || cfg.Inputs[0].ANSI != ANSIStrip {
		t.Fatalf("input0=%#v", cfg.Inputs[0])
	}
	if cfg.Inputs[1].Source != "/var/log/other.log" || cfg.Inputs[1].QueueRecords != 24 || cfg.Inputs[1].QueueBytes != 2*1024*1024 || cfg.Inputs[1].ANSI != ANSIKeep {
		t.Fatalf("input1=%#v", cfg.Inputs[1])
	}
	foundSource := false
	for _, field := range cfg.MetadataFields {
		if field == MetadataSource {
			foundSource = true
		}
	}
	if !foundSource {
		t.Fatalf("metadata missing source: %#v", cfg.MetadataFields)
	}
}

func TestParseConfigFileCustomMetadata(t *testing.T) {
	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "metadata.json")
	if err := os.WriteFile(metadataPath, []byte(`{"terraform_run":"first","retry":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := parseConfigFileData([]byte(`
custom_metadata_file = "` + strings.ReplaceAll(metadataPath, `\`, `\\`) + `"
custom_metadata = ["terraform_run=retry", "workspace=prod"]

[[inputs]]
path = "/var/log/app.log"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CustomMetadataFile != metadataPath {
		t.Fatalf("metadata file = %q", cfg.CustomMetadataFile)
	}
	if cfg.CustomMetadata["terraform_run"] != "retry" || cfg.CustomMetadata["workspace"] != "prod" || cfg.CustomMetadata["retry"] == nil {
		t.Fatalf("custom metadata = %#v", cfg.CustomMetadata)
	}
}

func TestParseConfigFileDefaultsToDropOldest(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
[[inputs]]
path = "/var/log/app.log"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Overflow != OverflowDropOldest {
		t.Fatalf("global overflow = %q", cfg.Overflow)
	}
	if len(cfg.Inputs) != 1 || cfg.Inputs[0].Overflow != OverflowDropOldest {
		t.Fatalf("inputs = %#v", cfg.Inputs)
	}
}

func TestParseConfigFileDefaultsApplyAfterInputs(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
[[inputs]]
path = "/var/log/app.log"

[[inputs]]
path = "/var/log/quiet.log"
queue_records = 7
partial_flush_interval = "0"

[queue]
records = 12
bytes = "1M"
overflow = "drop-newest"
max_fragment_bytes = "4K"

[file]
start = "beginning"
poll_interval = "25ms"
partial_flush_interval = "250ms"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inputs[0].QueueRecords != 12 || cfg.Inputs[0].QueueBytes != 1024*1024 || cfg.Inputs[0].Overflow != OverflowDropNewest || cfg.Inputs[0].MaxFragmentBytes != 4*1024 {
		t.Fatalf("input0 queue defaults = %#v", cfg.Inputs[0])
	}
	if cfg.Inputs[0].FileStart != FileStartBeginning || cfg.Inputs[0].FilePollInterval != 25*time.Millisecond || cfg.Inputs[0].PartialFlushInterval != 250*time.Millisecond {
		t.Fatalf("input0 file defaults = %#v", cfg.Inputs[0])
	}
	if cfg.Inputs[1].QueueRecords != 7 || cfg.Inputs[1].PartialFlushInterval != 0 {
		t.Fatalf("input1 overrides = %#v", cfg.Inputs[1])
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseConfigFileRejectsInvalidConfig(t *testing.T) {
	tests := []string{
		`format = "json"`,
		`[[inputs]]
path = ""`,
		`[queue]
overflow = "block"
[[inputs]]
path = "/tmp/x"`,
		`command = "echo"
[[inputs]]
path = "/tmp/x"`,
		`[unknown]
value = "x"
[[inputs]]
path = "/tmp/x"`,
		`metadata_interval = "2s"
[[inputs]]
path = "/tmp/x"`,
		`ansi = "paint"
[[inputs]]
path = "/tmp/x"`,
	}
	for _, text := range tests {
		if _, err := parseConfigFileData([]byte(text)); err == nil {
			t.Fatalf("expected error for %q", text)
		}
	}
}

func TestParseConfigFileHTTPAuth(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
[output]
kind = "http"
target = "https://example.test/logs"
auth = "bearer"
auth_secret_file = "/etc/logsurge/secrets/foobar"

[[inputs]]
path = "/var/log/app.log"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != OutputHTTP || cfg.OutputTarget != "https://example.test/logs" {
		t.Fatalf("output=%q target=%q", cfg.Output, cfg.OutputTarget)
	}
	if cfg.HTTPAuth != HTTPAuthBearer || cfg.HTTPAuthSecretFile != "/etc/logsurge/secrets/foobar" {
		t.Fatalf("auth=%q secret=%q", cfg.HTTPAuth, cfg.HTTPAuthSecretFile)
	}
	cfg, err = parseConfigFileData([]byte(`
[output]
kind = "http"
target = "https://example.test/logs"
auth = "basic"
auth_secret_file = "/etc/logsurge/secrets/basic-password"

[[inputs]]
path = "/var/log/app.log"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAuth != HTTPAuthBasic || cfg.HTTPAuthSecretFile != "/etc/logsurge/secrets/basic-password" {
		t.Fatalf("auth=%q secret=%q", cfg.HTTPAuth, cfg.HTTPAuthSecretFile)
	}

	bad := []string{
		`[output]
kind = "http"
target = "https://example.test/logs"
auth = "custom"
auth_secret_file = "/tmp/secret"
[[inputs]]
path = "/tmp/x"`,
		`[output]
kind = "stdout"
auth = "bearer"
auth_secret_file = "/tmp/secret"
[[inputs]]
path = "/tmp/x"`,
	}
	for _, text := range bad {
		if _, err := parseConfigFileData([]byte(text)); err == nil {
			t.Fatalf("expected error for %q", text)
		}
	}
}

func TestParseConfigFileMultipleOutputs(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
format = "json"

[[outputs]]
kind = "stdout"

[[outputs]]
kind = "http"
target = "https://example.test/logs"
http_format = "gelf"
http_batch_records = 7
http_batch_bytes = "2M"
http_retries = 0

[[inputs]]
path = "/var/log/app.log"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outputs) != 2 {
		t.Fatalf("outputs=%#v", cfg.Outputs)
	}
	if cfg.Outputs[0].Kind != OutputStdout || cfg.Outputs[1].Kind != OutputHTTP || cfg.Outputs[1].Target != "https://example.test/logs" {
		t.Fatalf("outputs=%#v", cfg.Outputs)
	}
	if cfg.Outputs[1].HTTPFormat != HTTPFormatGELF || cfg.Outputs[1].HTTPBatchRecords != 7 || cfg.Outputs[1].HTTPBatchBytes != 2*1024*1024 || cfg.Outputs[1].HTTPRetries != 0 {
		t.Fatalf("http output=%#v", cfg.Outputs[1])
	}
}

func TestParseConfigFileJournalInput(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
format = "json"

[[inputs]]
kind = "journal"
directory = "/var/log/journal"
source = "journald"
start = "all"
cursor_file = "/var/lib/logsurge/journal.cursor"
queue_records = 32
queue_bytes = "1M"
overflow = "drop-oldest"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inputs) != 1 {
		t.Fatalf("inputs=%#v", cfg.Inputs)
	}
	input := cfg.Inputs[0]
	if input.Kind != InputKindJournal || input.Directory != "/var/log/journal" || input.Source != "journald" || input.JournalStart != JournalStartAll || input.CursorFile != "/var/lib/logsurge/journal.cursor" {
		t.Fatalf("input=%#v", input)
	}
}

func TestParseConfigFileJournalInputDefaults(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
[[inputs]]
kind = "journal"
`))
	if err != nil {
		t.Fatal(err)
	}
	input := cfg.Inputs[0]
	if input.Directory != "/var/log/journal" || input.Source != "journald" || input.JournalStart != JournalStartEnd {
		t.Fatalf("input=%#v", input)
	}
}

func TestParseConfigFileNetworkInputsAndHealth(t *testing.T) {
	cfg, err := parseConfigFileData([]byte(`
health_listen = "127.0.0.1:9099"

[[inputs]]
kind = "tcp"
listen = "127.0.0.1:5514"
source = "local-tcp"
max_connections = 7

[[inputs]]
kind = "udp"
listen = "127.0.0.1:5515"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HealthListen != "127.0.0.1:9099" {
		t.Fatalf("health = %q", cfg.HealthListen)
	}
	if cfg.Inputs[0].Kind != InputKindTCP || cfg.Inputs[0].Listen != "127.0.0.1:5514" || cfg.Inputs[0].MaxConnections != 7 || cfg.Inputs[0].Source != "local-tcp" {
		t.Fatalf("tcp input = %#v", cfg.Inputs[0])
	}
	if cfg.Inputs[1].Kind != InputKindUDP || cfg.Inputs[1].Listen != "127.0.0.1:5515" || cfg.Inputs[1].Source != "udp://127.0.0.1:5515" {
		t.Fatalf("udp input = %#v", cfg.Inputs[1])
	}
}

func TestParseConfigFileRejectsBadNetworkInput(t *testing.T) {
	tests := []string{
		`[[inputs]]
kind = "tcp"`,
		`[[inputs]]
kind = "tcp"
listen = "0.0.0.0:5514"`,
		`[[inputs]]
kind = "udp"
listen = "127.0.0.1:5515"
max_connections = 7`,
		`health_listen = "0.0.0.0:9099"
[[inputs]]
path = "/tmp/x"`,
	}
	for _, text := range tests {
		if _, err := parseConfigFileData([]byte(text)); err == nil {
			t.Fatalf("expected error for %q", text)
		}
	}
}

func TestParseConfigFileRejectsBadJournalInput(t *testing.T) {
	tests := []string{
		`[[inputs]]
kind = "journal"
path = "/var/log/app.log"`,
		`[[inputs]]
kind = "wat"`,
		`[[inputs]]
kind = "journal"
start = "beginning"`,
		`[[inputs]]
kind = "file"
directory = "/var/log/journal"
path = "/tmp/x"`,
		`[[inputs]]
kind = "tcp"
listen = "127.0.0.1:5514"
cursor_file = "/tmp/cursor"`,
	}
	for _, text := range tests {
		if _, err := parseConfigFileData([]byte(text)); err == nil {
			t.Fatalf("expected error for %q", text)
		}
	}
}

func TestParseConfigFileRejectsBadMultipleOutputs(t *testing.T) {
	tests := []string{
		`[output]
kind = "stdout"
[[outputs]]
kind = "http"
target = "https://example.test/logs"
[[inputs]]
path = "/tmp/x"`,
		`[[outputs]]
kind = "stdout"
[[outputs]]
kind = "stdout"
[[inputs]]
path = "/tmp/x"`,
		`[[outputs]]
kind = "http"
[[inputs]]
path = "/tmp/x"`,
		`[[outputs]]
kind = "http"
target = "https://example.test/logs"
http_format = "otlp"
[[inputs]]
path = "/tmp/x"`,
	}
	for _, text := range tests {
		if _, err := parseConfigFileData([]byte(text)); err == nil {
			t.Fatalf("expected error for %q", text)
		}
	}
}

func TestParseConfigFlagSelectsConfigMode(t *testing.T) {
	path := writeTempConfig(t, `[[inputs]]
path = "/tmp/app.log"
`)
	cfg, err := ParseConfig([]string{"--config", path, "--debug-cpuprofile", "/tmp/cpu.pprof"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ConfigMode || cfg.ConfigPath != path || cfg.DebugCPUProfile != "/tmp/cpu.pprof" {
		t.Fatalf("cfg=%#v", cfg)
	}
}

func TestParseConfigFlagRejectsAdHocMixing(t *testing.T) {
	path := writeTempConfig(t, `[[inputs]]
path = "/tmp/app.log"
`)
	bad := [][]string{
		{"--config", path, "--", "echo"},
		{"--config", path, "--file", "/tmp/x"},
		{"--config", path, "--output", "dir"},
	}
	for _, args := range bad {
		if _, err := ParseConfig(args); err == nil {
			t.Fatalf("expected error for %s", strings.Join(args, " "))
		}
	}
}
