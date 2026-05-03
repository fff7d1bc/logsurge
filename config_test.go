package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"--output", "stdout",
		"--output-format", "json",
		"--queue-records", "12",
		"--queue-bytes", "2M",
		"--overflow", "block",
		"--max-fragment-bytes", "4K",
		"--partial-flush-interval", "250ms",
		"--flush-interval", "50ms",
		"--post-exit-drain-timeout", "1s",
		"--dir-max-bytes", "3M",
		"--dir-max-files", "7",
		"--http-batch-records", "9",
		"--http-batch-bytes", "5M",
		"--http-timeout", "3s",
		"--http-retries", "4",
		"--ansi", "keep",
		"--",
		"printf", "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Format != FormatJSON {
		t.Fatalf("format = %q", cfg.Format)
	}
	if cfg.QueueRecords != 12 {
		t.Fatalf("queue records = %d", cfg.QueueRecords)
	}
	if cfg.QueueBytes != 2*1024*1024 {
		t.Fatalf("queue bytes = %d", cfg.QueueBytes)
	}
	if cfg.Overflow != OverflowBlock {
		t.Fatalf("overflow = %q", cfg.Overflow)
	}
	if cfg.MaxFragmentBytes != 4*1024 {
		t.Fatalf("max fragment = %d", cfg.MaxFragmentBytes)
	}
	if cfg.PartialFlushInterval != 250*time.Millisecond {
		t.Fatalf("partial interval = %s", cfg.PartialFlushInterval)
	}
	if cfg.DirMaxBytes != 3*1024*1024 || cfg.DirMaxFiles != 7 {
		t.Fatalf("dir config = %d/%d", cfg.DirMaxBytes, cfg.DirMaxFiles)
	}
	if cfg.HTTPBatchRecords != 9 || cfg.HTTPBatchBytes != 5*1024*1024 || cfg.HTTPTimeout != 3*time.Second || cfg.HTTPRetries != 4 {
		t.Fatalf("http config = records=%d bytes=%d timeout=%s retries=%d", cfg.HTTPBatchRecords, cfg.HTTPBatchBytes, cfg.HTTPTimeout, cfg.HTTPRetries)
	}
	if cfg.ANSI != ANSIKeep {
		t.Fatalf("ansi = %q", cfg.ANSI)
	}
	if !reflect.DeepEqual(cfg.Command, []string{"printf", "x"}) {
		t.Fatalf("command = %#v", cfg.Command)
	}
	if cfg.Source != SourceExec {
		t.Fatalf("source = %q", cfg.Source)
	}
}

func TestParseConfigDefaultsToStdin(t *testing.T) {
	cfg, err := ParseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != SourceStdin {
		t.Fatalf("source = %q", cfg.Source)
	}
	if cfg.Overflow != OverflowBlock {
		t.Fatalf("overflow = %q", cfg.Overflow)
	}
}

func TestParseConfigRejectsPositionalArgsWithoutSeparator(t *testing.T) {
	if _, err := ParseConfig([]string{"echo", "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseConfigFileSource(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"--file", "/tmp/app.log",
		"--file-start", "beginning",
		"--file-poll-interval", "25ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != SourceFile || cfg.FilePath != "/tmp/app.log" {
		t.Fatalf("source = %q file=%q", cfg.Source, cfg.FilePath)
	}
	if cfg.FileStart != FileStartBeginning {
		t.Fatalf("file start = %q", cfg.FileStart)
	}
	if cfg.FilePollInterval != 25*time.Millisecond {
		t.Fatalf("poll = %s", cfg.FilePollInterval)
	}
}

func TestParseConfigListenSource(t *testing.T) {
	cfg, err := ParseConfig([]string{"--listen", "tcp://127.0.0.1:5514", "--health-listen", "127.0.0.1:9099"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != SourceListen || cfg.ListenNetwork != "tcp" || cfg.ListenAddress != "127.0.0.1:5514" {
		t.Fatalf("listen cfg = %#v", cfg)
	}
	if cfg.Overflow != OverflowDropOldest {
		t.Fatalf("network overflow = %q", cfg.Overflow)
	}
	if cfg.HealthListen != "127.0.0.1:9099" {
		t.Fatalf("health = %q", cfg.HealthListen)
	}

	cfg, err = ParseConfig([]string{"--listen", "udp://127.0.0.1:5515", "--overflow", "drop-newest"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenNetwork != "udp" || cfg.Overflow != OverflowDropNewest {
		t.Fatalf("listen cfg = %#v", cfg)
	}
}

func TestParseConfigRejectsBadListen(t *testing.T) {
	tests := [][]string{
		{"--listen", "http://127.0.0.1:5514"},
		{"--listen", "tcp://0.0.0.0:5514"},
		{"--listen", "tcp://127.0.0.1:5514", "--", "echo"},
		{"--listen", "tcp://127.0.0.1:5514", "--file", "/tmp/app.log"},
		{"--health-listen", "0.0.0.0:9099"},
	}
	for _, args := range tests {
		if _, err := ParseConfig(args); err == nil {
			t.Fatalf("expected error for %#v", args)
		}
	}
}

func TestParseConfigShortAliases(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"-F", "json",
		"-m", "source,line_end",
		"-o", "dir=/tmp/logsurge",
		"--",
		"printf", "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Format != FormatJSON {
		t.Fatalf("format = %q", cfg.Format)
	}
	if cfg.Output != OutputDir || cfg.OutputTarget != "/tmp/logsurge" {
		t.Fatalf("output = %q target=%q", cfg.Output, cfg.OutputTarget)
	}
	if len(cfg.MetadataFields) != 2 || cfg.MetadataFields[0] != MetadataSource || cfg.MetadataFields[1] != MetadataLineEnd {
		t.Fatalf("metadata fields = %#v", cfg.MetadataFields)
	}
	if !reflect.DeepEqual(cfg.Command, []string{"printf", "x"}) {
		t.Fatalf("command = %#v", cfg.Command)
	}
}

func TestParseConfigShortAliasesWithEquals(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"-F=json",
		"-m=source,line_end",
		"-o=dir=/tmp/logsurge",
		"--",
		"printf", "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Format != FormatJSON || cfg.Output != OutputDir || cfg.OutputTarget != "/tmp/logsurge" {
		t.Fatalf("cfg = %#v", cfg)
	}
	if len(cfg.MetadataFields) != 2 || cfg.MetadataFields[0] != MetadataSource || cfg.MetadataFields[1] != MetadataLineEnd {
		t.Fatalf("metadata fields = %#v", cfg.MetadataFields)
	}
}

func TestParseConfigMultipleOutputs(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"--http-format", "gelf",
		"--output", "stdout",
		"--output", "http=https://example.test/logs",
		"--",
		"echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outputs) != 2 {
		t.Fatalf("outputs = %#v", cfg.Outputs)
	}
	if cfg.Outputs[0].Kind != OutputStdout || cfg.Outputs[1].Kind != OutputHTTP || cfg.Outputs[1].Target != "https://example.test/logs" || cfg.Outputs[1].HTTPFormat != HTTPFormatGELF {
		t.Fatalf("outputs = %#v", cfg.Outputs)
	}
	if cfg.Outputs[1].HTTPBatchRecords != 1 {
		t.Fatalf("gelf batch records = %d", cfg.Outputs[1].HTTPBatchRecords)
	}
	if cfg.Output != OutputStdout {
		t.Fatalf("compat output = %q", cfg.Output)
	}
}

func TestParseConfigCustomMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(path, []byte(`{"terraform_run":"first","retry":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig([]string{
		"--metadata-file", path,
		"--metadata-field", "terraform_run=retry",
		"--metadata-field", "workspace=prod",
		"--",
		"echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CustomMetadata["terraform_run"] != "retry" || cfg.CustomMetadata["workspace"] != "prod" || cfg.CustomMetadata["retry"] == nil {
		t.Fatalf("custom metadata = %#v", cfg.CustomMetadata)
	}
}

func TestParseConfigRejectsBadCustomMetadata(t *testing.T) {
	for _, args := range [][]string{
		{"--metadata-field", "source=x", "--", "echo"},
		{"--metadata-field", "bad", "--", "echo"},
	} {
		if _, err := ParseConfig(args); err == nil {
			t.Fatalf("expected error for %#v", args)
		}
	}
}

func TestParseConfigInlineOutputTargets(t *testing.T) {
	cfg, err := ParseConfig([]string{"--output", "dir=/tmp/logsurge", "--", "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Outputs) != 1 || cfg.Outputs[0].Kind != OutputDir || cfg.Outputs[0].Target != "/tmp/logsurge" {
		t.Fatalf("outputs = %#v", cfg.Outputs)
	}
}

func TestParseConfigRejectsBadMultipleOutputs(t *testing.T) {
	tests := [][]string{
		{"--output", "stdout", "--output", "stdout", "--", "echo"},
		{"--output", "http", "--", "echo"},
		{"--output", "http=/tmp/logs", "--", "echo"},
		{"--output", "stdout=/tmp/logs", "--", "echo"},
		{"--output-target", "/tmp/logs", "--", "echo"},
		{"--http-format", "otlp", "--output", "http=https://example.test/logs", "--", "echo"},
	}
	for _, args := range tests {
		if _, err := ParseConfig(args); err == nil {
			t.Fatalf("expected error for %#v", args)
		}
	}
}

func TestParseConfigShortFileAlias(t *testing.T) {
	cfg, err := ParseConfig([]string{"-f", "/tmp/app.log"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != SourceFile || cfg.FilePath != "/tmp/app.log" {
		t.Fatalf("source = %q file=%q", cfg.Source, cfg.FilePath)
	}
}

func TestParseConfigShortConfigAlias(t *testing.T) {
	for _, args := range [][]string{
		{"-c", writeTempConfig(t, `[[inputs]]
path = "/var/log/app.log"
`)},
		{"-c=" + writeTempConfig(t, `[[inputs]]
path = "/var/log/other.log"
`)},
	} {
		cfg, err := ParseConfig(args)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.ConfigMode || cfg.ConfigPath == "" || cfg.Source != SourceFile {
			t.Fatalf("cfg = %#v", cfg)
		}
	}
}

func TestParseConfigShortAliasesUseExistingValidation(t *testing.T) {
	tests := [][]string{
		{"-F", "xml", "--", "echo"},
		{"-m", "time", "--", "echo"},
		{"-o", "file", "--", "echo"},
		{"-t", "/tmp/logsurge", "--", "echo"},
	}
	for _, args := range tests {
		if _, err := ParseConfig(args); err == nil {
			t.Fatalf("expected error for %#v", args)
		}
	}
}

func TestParseConfigRejectsFileWithCommand(t *testing.T) {
	if _, err := ParseConfig([]string{"--file", "/tmp/app.log", "--", "echo"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseConfigRejectsBadFileOptions(t *testing.T) {
	if _, err := ParseConfig([]string{"--file", "/tmp/app.log", "--file-start", "middle"}); err == nil {
		t.Fatal("expected file-start error")
	}
	if _, err := ParseConfig([]string{"--file", "/tmp/app.log", "--file-poll-interval", "0"}); err == nil {
		t.Fatal("expected poll interval error")
	}
}

func TestParseConfigRejectsFragmentLargerThanQueue(t *testing.T) {
	_, err := ParseConfig([]string{
		"--queue-bytes", "10",
		"--max-fragment-bytes", "11",
		"--",
		"echo",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseConfigHelpAndVersion(t *testing.T) {
	if _, err := ParseConfig([]string{"--help"}); !errors.Is(err, ErrHelp) {
		t.Fatalf("help err = %v", err)
	}
	cfg, err := ParseConfig([]string{"--version"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Version {
		t.Fatal("version flag not set")
	}
	if !strings.Contains(Usage(), "--overflow drop-oldest|drop-newest|block") {
		t.Fatal("usage missing overflow flag")
	}
	if !strings.Contains(Usage(), "--output KIND[=TARGET]") {
		t.Fatal("usage missing sink flag")
	}
	if !strings.Contains(Usage(), "--file PATH") {
		t.Fatal("usage missing file flag")
	}
	if !strings.Contains(Usage(), "--ansi strip|keep") {
		t.Fatal("usage missing ansi flag")
	}
}

func TestParseConfigOutputTargetValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "legacy output target rejected",
			args: []string{"--output", "stdout", "--output-target", "/tmp/x", "--", "echo"},
		},
		{
			name: "dir target required",
			args: []string{"--output", "dir", "--", "echo"},
		},
		{
			name: "http target required",
			args: []string{"--output", "http", "--", "echo"},
		},
		{
			name: "http target scheme required",
			args: []string{"--output", "http=/tmp/x", "--", "echo"},
		},
		{
			name: "unknown output rejected",
			args: []string{"--output", "file", "--", "echo"},
		},
		{
			name: "http auth rejected for stdout",
			args: []string{"--http-auth", "bearer", "--http-auth-secret-file", "/tmp/secret", "--", "echo"},
		},
		{
			name: "http auth secret requires bearer",
			args: []string{"--output", "http=https://example.test/logs", "--http-auth-secret-file", "/tmp/secret", "--", "echo"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseConfig(tt.args); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	cfg, err := ParseConfig([]string{"--output", "dir=/tmp/logsurge", "--", "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != OutputDir || cfg.OutputTarget != "/tmp/logsurge" {
		t.Fatalf("output = %q target=%q", cfg.Output, cfg.OutputTarget)
	}
	cfg, err = ParseConfig([]string{"--output", "http=https://example.test/logs", "--", "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != OutputHTTP || cfg.OutputTarget != "https://example.test/logs" {
		t.Fatalf("output = %q target=%q", cfg.Output, cfg.OutputTarget)
	}
	cfg, err = ParseConfig([]string{"--output", "http=https://example.test/logs", "--http-auth", "bearer", "--http-auth-secret-file", "/tmp/secret", "--", "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAuth != HTTPAuthBearer || cfg.HTTPAuthSecretFile != "/tmp/secret" {
		t.Fatalf("http auth = %q secret=%q", cfg.HTTPAuth, cfg.HTTPAuthSecretFile)
	}
	cfg, err = ParseConfig([]string{"--output", "http=https://example.test/logs", "--http-auth", "basic", "--http-auth-secret-file", "/tmp/secret", "--", "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAuth != HTTPAuthBasic || cfg.HTTPAuthSecretFile != "/tmp/secret" {
		t.Fatalf("http auth = %q secret=%q", cfg.HTTPAuth, cfg.HTTPAuthSecretFile)
	}
	cfg, err = ParseConfig([]string{"--output", "http=https://example.test/logs", "--http-auth", "bearer", "--", "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAuth != HTTPAuthBearer || cfg.HTTPAuthSecretFile != "" {
		t.Fatalf("http auth = %q secret=%q", cfg.HTTPAuth, cfg.HTTPAuthSecretFile)
	}
	cfg, err = ParseConfig([]string{"--output", "http=https://example.test/logs", "--http-auth", "basic", "--", "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAuth != HTTPAuthBasic || cfg.HTTPAuthSecretFile != "" {
		t.Fatalf("http auth = %q secret=%q", cfg.HTTPAuth, cfg.HTTPAuthSecretFile)
	}
	if _, err := ParseConfig([]string{"--output", "http=https://example.test/logs", "--http-auth", "custom", "--http-auth-secret-file", "/tmp/secret", "--", "echo"}); err == nil {
		t.Fatal("expected unsupported auth error")
	}
}

func TestParseConfigMetadataFields(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"--format", "json",
		"--metadata", "hostname,source,line_end,continued",
		"--",
		"echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MetadataFields) != 4 {
		t.Fatalf("fields = %#v", cfg.MetadataFields)
	}
	if cfg.MetadataFields[0] != MetadataHostname || cfg.MetadataFields[1] != MetadataSource || cfg.MetadataFields[2] != MetadataLineEnd || cfg.MetadataFields[3] != MetadataContinued {
		t.Fatalf("fields = %#v", cfg.MetadataFields)
	}
}

func TestParseConfigRejectsBadMetadataField(t *testing.T) {
	_, err := ParseConfig([]string{
		"--metadata", "time",
		"--",
		"echo",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseConfigRejectsBadANSI(t *testing.T) {
	_, err := ParseConfig([]string{
		"--ansi", "paint",
		"--",
		"echo",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseConfigRejectsMetadataInterval(t *testing.T) {
	if _, err := ParseConfig([]string{"--metadata-interval", "2s", "--", "echo"}); err == nil {
		t.Fatal("expected error")
	}
}
