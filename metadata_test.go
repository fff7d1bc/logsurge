package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMetadataFields(t *testing.T) {
	fields, err := ParseMetadataFields("hostname,source,lineEnd,continued,hostname")
	if err != nil {
		t.Fatal(err)
	}
	want := []MetadataField{MetadataHostname, MetadataSource, MetadataLineEnd, MetadataContinued}
	if len(fields) != len(want) {
		t.Fatalf("len = %d fields=%#v", len(fields), fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Fatalf("fields = %#v", fields)
		}
	}
}

func TestParseMetadataFieldsRejectsMetrics(t *testing.T) {
	for _, spec := range []string{"memory", "loadavg", "cpu"} {
		if _, err := ParseMetadataFields(spec); err == nil {
			t.Fatalf("expected error for %q", spec)
		}
	}
}

func TestParseMetadataFieldsRejectsBadField(t *testing.T) {
	if _, err := ParseMetadataFields("hostname,nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestStaticMetadataSnapshot(t *testing.T) {
	snapshot := staticMetadataSnapshot([]MetadataField{MetadataHostname, MetadataSource, MetadataLineEnd}, map[string]any{"run": "retry"})
	if _, ok := snapshot["hostname"]; !ok {
		t.Fatalf("snapshot missing hostname: %#v", snapshot)
	}
	if snapshot["run"] != "retry" {
		t.Fatalf("snapshot missing custom metadata: %#v", snapshot)
	}
	if _, ok := snapshot["source"]; ok {
		t.Fatalf("snapshot included per-record source: %#v", snapshot)
	}
}

func TestLoadCustomMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(path, []byte(`{"terraform_run":"first","retry":1,"dry_run":true,"note":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata, err := loadCustomMetadata(path, []string{"terraform_run=retry", "operator=ci"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata["terraform_run"] != "retry" || metadata["operator"] != "ci" || metadata["dry_run"] != true || metadata["note"] != nil {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestLoadCustomMetadataRejectsBadInput(t *testing.T) {
	for name, content := range map[string]string{
		"array":  `[]`,
		"nested": `{"run":{"nested":true}}`,
		"key":    `{"_run":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "metadata.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadCustomMetadata(path, nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	for _, pair := range []string{"missing_equals", "source=x", "9run=x"} {
		if _, err := loadCustomMetadata("", []string{pair}); err == nil {
			t.Fatalf("expected error for %q", pair)
		}
	}
}
