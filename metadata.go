package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type MetadataField string

const (
	MetadataHostname  MetadataField = "hostname"
	MetadataSource    MetadataField = "source"
	MetadataLineEnd   MetadataField = "line_end"
	MetadataContinued MetadataField = "continued"
)

func ParseMetadataFields(spec string) ([]MetadataField, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	parts := strings.Split(spec, ",")
	return ParseMetadataFieldList(parts)
}

func ParseMetadataFieldList(parts []string) ([]MetadataField, error) {
	fields := make([]MetadataField, 0, len(parts))
	seen := make(map[MetadataField]struct{}, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			return nil, errors.New("empty metadata field")
		}
		field := normalizeMetadataField(part)
		switch field {
		case MetadataHostname, MetadataSource, MetadataLineEnd, MetadataContinued:
		default:
			return nil, errors.New("unsupported metadata field " + part)
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	return fields, nil
}

func appendMetadataField(fields []MetadataField, field MetadataField) []MetadataField {
	for _, existing := range fields {
		if existing == field {
			return fields
		}
	}
	return append(fields, field)
}

func normalizeMetadataField(s string) MetadataField {
	switch s {
	case "lineend":
		return MetadataLineEnd
	default:
		return MetadataField(s)
	}
}

func staticMetadataSnapshot(fields []MetadataField, custom map[string]any) map[string]any {
	out := copyMetadataMap(custom)
	for _, field := range fields {
		switch field {
		case MetadataHostname:
			if out == nil {
				out = make(map[string]any)
			}
			// Hostname is process-static metadata here. Metrics belong in a
			// separate telemetry path and can be correlated by timestamps.
			hostname, err := os.Hostname()
			if err != nil {
				out["hostname"] = nil
			} else {
				out["hostname"] = hostname
			}
		}
	}
	return out
}

func loadCustomMetadata(path string, pairs []string) (map[string]any, error) {
	var out map[string]any
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var obj map[string]any
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%s: expected one JSON object", path)
		}
		if obj == nil {
			return nil, fmt.Errorf("%s: expected flat JSON object", path)
		}
		for key, value := range obj {
			if err := validateCustomMetadataKey(key); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if err := validateCustomMetadataValue(key, value); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if out == nil {
				out = make(map[string]any, len(obj))
			}
			out[key] = value
		}
	}
	for _, pair := range pairs {
		key, value, err := parseCustomMetadataPair(pair)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = make(map[string]any, len(pairs))
		}
		out[key] = value
	}
	return out, nil
}

func parseCustomMetadataPair(pair string) (string, string, error) {
	key, value, ok := strings.Cut(pair, "=")
	if !ok {
		return "", "", fmt.Errorf("custom metadata %q must be KEY=VALUE", pair)
	}
	key = strings.TrimSpace(key)
	if err := validateCustomMetadataKey(key); err != nil {
		return "", "", err
	}
	return key, value, nil
}

func validateCustomMetadataValue(key string, value any) error {
	switch value.(type) {
	case nil, string, bool, json.Number, float64, int, int64, uint64:
		return nil
	default:
		return fmt.Errorf("custom metadata %q must be string, number, bool, or null", key)
	}
}

func validateCustomMetadataKey(key string) error {
	if key == "" {
		return errors.New("custom metadata key must not be empty")
	}
	if isReservedCustomMetadataKey(key) {
		return fmt.Errorf("custom metadata key %q is reserved", key)
	}
	for i, r := range key {
		if r > 127 {
			return fmt.Errorf("custom metadata key %q must be ASCII", key)
		}
		if i == 0 {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				return fmt.Errorf("custom metadata key %q must start with a letter", key)
			}
			continue
		}
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return fmt.Errorf("custom metadata key %q contains unsupported character %q", key, r)
		}
	}
	return nil
}

func isReservedCustomMetadataKey(key string) bool {
	switch key {
	case "time", "line", "metadata", "_time", "_msg",
		"hostname", "source", "line_end", "continued",
		"version", "host", "short_message", "timestamp",
		"event", "records", "bytes", "reason",
		"_hostname", "_source", "_line_end", "_continued",
		"_event", "_records", "_bytes", "_reason":
		return true
	default:
		return strings.HasPrefix(key, "_")
	}
}

func copyMetadataMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
