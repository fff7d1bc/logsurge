package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type configSection string

const (
	configSectionRoot    configSection = "root"
	configSectionOutput  configSection = "output"
	configSectionOutputs configSection = "outputs"
	configSectionQueue   configSection = "queue"
	configSectionFile    configSection = "file"
	configSectionInput   configSection = "input"
)

func ParseConfigFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return parseConfigFileData(data)
}

func parseConfigFileData(data []byte) (Config, error) {
	cfg := DefaultConfig()
	cfg.ConfigMode = true
	cfg.Source = SourceFile
	cfg.Output = OutputStdout
	cfg.Overflow = OverflowDropOldest
	// Daemon mode has many inputs by design. Include source metadata unless the
	// user opts out so JSON/plain records can still be attributed downstream.
	cfg.MetadataFields = appendMetadataField(cfg.MetadataFields, MetadataSource)

	section := configSectionRoot
	currentInput := -1
	currentOutput := -1
	hasOutputSection := false
	hasOutputsArray := false
	var customMetadataPairs []string
	var customMetadataFile string

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(stripTOMLComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]"))
			switch name {
			case "inputs":
				cfg.Inputs = append(cfg.Inputs, InputConfig{})
				currentInput = len(cfg.Inputs) - 1
				currentOutput = -1
				section = configSectionInput
			case "outputs":
				if hasOutputSection {
					return cfg, fmt.Errorf("line %d: [output] cannot be combined with [[outputs]]", lineNo)
				}
				hasOutputsArray = true
				cfg.Outputs = append(cfg.Outputs, outputDefaultsFromConfig(cfg))
				currentOutput = len(cfg.Outputs) - 1
				currentInput = -1
				section = configSectionOutputs
			default:
				return cfg, fmt.Errorf("line %d: unsupported array section %q", lineNo, name)
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			switch name {
			case "output":
				if hasOutputsArray {
					return cfg, fmt.Errorf("line %d: [output] cannot be combined with [[outputs]]", lineNo)
				}
				hasOutputSection = true
				section = configSectionOutput
			case "queue":
				section = configSectionQueue
			case "file":
				section = configSectionFile
			default:
				return cfg, fmt.Errorf("line %d: unsupported section %q", lineNo, name)
			}
			currentInput = -1
			currentOutput = -1
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, fmt.Errorf("line %d: expected key = value", lineNo)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return cfg, fmt.Errorf("line %d: expected key = value", lineNo)
		}
		var err error
		switch section {
		case configSectionRoot:
			switch key {
			case "custom_metadata":
				var values []string
				values, err = parseTOMLStringArray(value)
				customMetadataPairs = append(customMetadataPairs, values...)
			case "custom_metadata_file":
				customMetadataFile, err = parseTOMLString(value)
			case "health_listen":
				cfg.HealthListen, err = parseTOMLString(value)
			default:
				err = applyRootConfigValue(&cfg, key, value)
			}
			if key == "metadata" {
				cfg.MetadataFields = appendMetadataField(cfg.MetadataFields, MetadataSource)
			}
		case configSectionOutput:
			err = applyOutputConfigValue(&cfg, key, value)
		case configSectionOutputs:
			if currentOutput < 0 {
				err = errors.New("output value without [[outputs]]")
			} else {
				err = applyOutputConfigValueToOutput(&cfg.Outputs[currentOutput], key, value)
			}
		case configSectionQueue:
			err = applyQueueConfigValue(&cfg, key, value)
		case configSectionFile:
			err = applyFileConfigValue(&cfg, key, value)
		case configSectionInput:
			if currentInput < 0 {
				err = errors.New("input value without [[inputs]]")
			} else {
				err = applyInputConfigValue(&cfg.Inputs[currentInput], key, value)
			}
		}
		if err != nil {
			return cfg, fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}
	cfg.CustomMetadataFile = customMetadataFile
	var customErr error
	if cfg.CustomMetadata, customErr = loadCustomMetadata(customMetadataFile, customMetadataPairs); customErr != nil {
		return cfg, fmt.Errorf("invalid custom metadata: %w", customErr)
	}
	return validateConfigFile(cfg)
}

func outputDefaultsFromConfig(cfg Config) OutputConfig {
	httpBatchRecords := 0
	if cfg.HTTPBatchRecordsSet {
		httpBatchRecords = cfg.HTTPBatchRecords
	}
	httpBatchBytes := 0
	if cfg.HTTPBatchBytesSet {
		httpBatchBytes = cfg.HTTPBatchBytes
	}
	return OutputConfig{
		DirMaxBytes:         cfg.DirMaxBytes,
		DirMaxFiles:         cfg.DirMaxFiles,
		DirMaxFilesSet:      true,
		HTTPBatchRecords:    httpBatchRecords,
		HTTPBatchRecordsSet: cfg.HTTPBatchRecordsSet,
		HTTPBatchBytes:      httpBatchBytes,
		HTTPBatchBytesSet:   cfg.HTTPBatchBytesSet,
		HTTPTimeout:         cfg.HTTPTimeout,
		HTTPRetries:         cfg.HTTPRetries,
		HTTPRetriesSet:      true,
		HTTPAuth:            cfg.HTTPAuth,
		HTTPAuthSecretFile:  cfg.HTTPAuthSecretFile,
		HTTPFormat:          cfg.HTTPFormat,
	}
}

func applyRootConfigValue(cfg *Config, key string, value string) error {
	switch key {
	case "format":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		switch Format(v) {
		case FormatPlain, FormatJSON:
			cfg.Format = Format(v)
			return nil
		default:
			return fmt.Errorf("unsupported format %q", v)
		}
	case "metadata":
		values, err := parseTOMLStringArray(value)
		if err != nil {
			return err
		}
		fields, err := ParseMetadataFieldList(values)
		if err != nil {
			return err
		}
		cfg.MetadataFields = fields
		return nil
	case "ansi":
		ansi, err := parseTOMLANSI(value)
		if err != nil {
			return err
		}
		cfg.ANSI = ansi
		return nil
	case "flush_interval":
		d, err := parseTOMLDuration(value)
		if err != nil {
			return err
		}
		cfg.FlushInterval = d
		return nil
	default:
		return fmt.Errorf("unsupported root key %q", key)
	}
}

func applyOutputConfigValue(cfg *Config, key string, value string) error {
	output := OutputConfig{
		Kind:                cfg.Output,
		Target:              cfg.OutputTarget,
		DirMaxBytes:         cfg.DirMaxBytes,
		DirMaxFiles:         cfg.DirMaxFiles,
		HTTPBatchRecords:    cfg.HTTPBatchRecords,
		HTTPBatchRecordsSet: cfg.HTTPBatchRecordsSet,
		HTTPBatchBytes:      cfg.HTTPBatchBytes,
		HTTPBatchBytesSet:   cfg.HTTPBatchBytesSet,
		HTTPTimeout:         cfg.HTTPTimeout,
		HTTPRetries:         cfg.HTTPRetries,
		HTTPAuth:            cfg.HTTPAuth,
		HTTPAuthSecretFile:  cfg.HTTPAuthSecretFile,
		HTTPFormat:          cfg.HTTPFormat,
	}
	if err := applyOutputConfigValueToOutput(&output, key, value); err != nil {
		return err
	}
	cfg.Output = output.Kind
	cfg.OutputTarget = output.Target
	cfg.DirMaxBytes = output.DirMaxBytes
	cfg.DirMaxFiles = output.DirMaxFiles
	cfg.HTTPBatchRecords = output.HTTPBatchRecords
	cfg.HTTPBatchRecordsSet = output.HTTPBatchRecordsSet
	cfg.HTTPBatchBytes = output.HTTPBatchBytes
	cfg.HTTPBatchBytesSet = output.HTTPBatchBytesSet
	cfg.HTTPTimeout = output.HTTPTimeout
	cfg.HTTPRetries = output.HTTPRetries
	cfg.HTTPAuth = output.HTTPAuth
	cfg.HTTPAuthSecretFile = output.HTTPAuthSecretFile
	cfg.HTTPFormat = output.HTTPFormat
	return nil
}

func applyOutputConfigValueToOutput(output *OutputConfig, key string, value string) error {
	switch key {
	case "kind":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		switch OutputKind(v) {
		case OutputStdout, OutputDir, OutputHTTP:
			output.Kind = OutputKind(v)
			return nil
		default:
			return fmt.Errorf("unsupported output %q", v)
		}
	case "target":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		output.Target = v
		return nil
	case "dir_max_bytes":
		n, err := parseTOMLBytes(value)
		if err != nil {
			return err
		}
		output.DirMaxBytes = n
		return nil
	case "dir_max_files":
		n, err := parseTOMLInt(value)
		if err != nil {
			return err
		}
		output.DirMaxFiles = n
		output.DirMaxFilesSet = true
		return nil
	case "http_batch_records":
		n, err := parseTOMLInt(value)
		if err != nil {
			return err
		}
		output.HTTPBatchRecords = n
		output.HTTPBatchRecordsSet = true
		return nil
	case "http_batch_bytes":
		n, err := parseTOMLBytes(value)
		if err != nil {
			return err
		}
		output.HTTPBatchBytes = n
		output.HTTPBatchBytesSet = true
		return nil
	case "http_format":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		switch HTTPFormat(v) {
		case HTTPFormatJSONLine, HTTPFormatGELF:
			output.HTTPFormat = HTTPFormat(v)
			return nil
		default:
			return fmt.Errorf("unsupported HTTP format %q", v)
		}
	case "http_timeout":
		d, err := parseTOMLDuration(value)
		if err != nil {
			return err
		}
		output.HTTPTimeout = d
		return nil
	case "http_retries":
		n, err := parseTOMLInt(value)
		if err != nil {
			return err
		}
		output.HTTPRetries = n
		output.HTTPRetriesSet = true
		return nil
	case "auth":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		switch HTTPAuthMode(v) {
		case HTTPAuthNone, HTTPAuthBearer, HTTPAuthBasic:
			output.HTTPAuth = HTTPAuthMode(v)
			return nil
		default:
			return fmt.Errorf("unsupported HTTP auth mode %q", v)
		}
	case "auth_secret_file":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		output.HTTPAuthSecretFile = v
		return nil
	default:
		return fmt.Errorf("unsupported output key %q", key)
	}
}

func applyQueueConfigValue(cfg *Config, key string, value string) error {
	switch key {
	case "records":
		n, err := parseTOMLInt(value)
		if err != nil {
			return err
		}
		cfg.QueueRecords = n
		return nil
	case "bytes":
		n, err := parseTOMLBytes(value)
		if err != nil {
			return err
		}
		cfg.QueueBytes = n
		return nil
	case "overflow":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		overflow, err := parseConfigOverflow(v)
		if err != nil {
			return err
		}
		cfg.Overflow = overflow
		return nil
	case "max_fragment_bytes":
		n, err := parseTOMLBytes(value)
		if err != nil {
			return err
		}
		cfg.MaxFragmentBytes = n
		return nil
	default:
		return fmt.Errorf("unsupported queue key %q", key)
	}
}

func applyFileConfigValue(cfg *Config, key string, value string) error {
	switch key {
	case "start":
		start, err := parseTOMLFileStart(value)
		if err != nil {
			return err
		}
		cfg.FileStart = start
		return nil
	case "poll_interval":
		d, err := parseTOMLDuration(value)
		if err != nil {
			return err
		}
		cfg.FilePollInterval = d
		return nil
	case "partial_flush_interval":
		d, err := parseTOMLDuration(value)
		if err != nil {
			return err
		}
		cfg.PartialFlushInterval = d
		return nil
	default:
		return fmt.Errorf("unsupported file key %q", key)
	}
}

func applyInputConfigValue(input *InputConfig, key string, value string) error {
	switch key {
	case "kind":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		switch InputKind(v) {
		case InputKindFile, InputKindJournal, InputKindTCP, InputKindUDP:
			input.Kind = InputKind(v)
			return nil
		default:
			return fmt.Errorf("unsupported input kind %q", v)
		}
	case "path":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		input.Path = v
		return nil
	case "listen":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		input.Listen = v
		return nil
	case "max_connections":
		n, err := parseTOMLInt(value)
		if err != nil {
			return err
		}
		input.MaxConnections = n
		input.MaxConnectionsSet = true
		return nil
	case "directory":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		input.Directory = v
		return nil
	case "start":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		switch JournalStart(v) {
		case JournalStartEnd, JournalStartAll:
			input.JournalStart = JournalStart(v)
			return nil
		default:
			return fmt.Errorf("unsupported journal start %q", v)
		}
	case "cursor_file":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		input.CursorFile = v
		return nil
	case "source":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		input.Source = v
		return nil
	case "ansi":
		ansi, err := parseTOMLANSI(value)
		if err != nil {
			return err
		}
		input.ANSI = ansi
		input.ANSISet = true
		return nil
	case "queue_records":
		n, err := parseTOMLInt(value)
		if err != nil {
			return err
		}
		input.QueueRecords = n
		input.QueueRecordsSet = true
		return nil
	case "queue_bytes":
		n, err := parseTOMLBytes(value)
		if err != nil {
			return err
		}
		input.QueueBytes = n
		input.QueueBytesSet = true
		return nil
	case "overflow":
		v, err := parseTOMLString(value)
		if err != nil {
			return err
		}
		overflow, err := parseConfigOverflow(v)
		if err != nil {
			return err
		}
		input.Overflow = overflow
		input.OverflowSet = true
		return nil
	case "max_fragment_bytes":
		n, err := parseTOMLBytes(value)
		if err != nil {
			return err
		}
		input.MaxFragmentBytes = n
		input.MaxFragmentBytesSet = true
		return nil
	case "partial_flush_interval":
		d, err := parseTOMLDuration(value)
		if err != nil {
			return err
		}
		input.PartialFlushInterval = d
		input.PartialFlushSet = true
		return nil
	case "file_start":
		start, err := parseTOMLFileStart(value)
		if err != nil {
			return err
		}
		input.FileStart = start
		input.FileStartSet = true
		return nil
	case "file_poll_interval":
		d, err := parseTOMLDuration(value)
		if err != nil {
			return err
		}
		input.FilePollInterval = d
		input.FilePollIntervalSet = true
		return nil
	default:
		return fmt.Errorf("unsupported input key %q", key)
	}
}

func parseTOMLANSI(value string) (ansiMode, error) {
	v, err := parseTOMLString(value)
	if err != nil {
		return "", err
	}
	switch ansiMode(v) {
	case ANSIStrip, ANSIKeep:
		return ansiMode(v), nil
	default:
		return "", fmt.Errorf("unsupported ANSI mode %q", v)
	}
}

func parseTOMLFileStart(value string) (FileStart, error) {
	v, err := parseTOMLString(value)
	if err != nil {
		return "", err
	}
	switch FileStart(v) {
	case FileStartBeginning, FileStartEnd:
		return FileStart(v), nil
	default:
		return "", fmt.Errorf("unsupported file start %q", v)
	}
}

func parseTOMLDuration(value string) (time.Duration, error) {
	v, err := parseTOMLString(value)
	if err != nil {
		return 0, err
	}
	return time.ParseDuration(v)
}

func parseTOMLBytes(value string) (int, error) {
	if strings.HasPrefix(value, `"`) {
		v, err := parseTOMLString(value)
		if err != nil {
			return 0, err
		}
		return parseBytes(v)
	}
	return parseBytes(value)
}

func parseTOMLInt(value string) (int, error) {
	n, err := strconv.ParseInt(value, 10, 0)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func parseTOMLString(value string) (string, error) {
	if !strings.HasPrefix(value, `"`) {
		return "", fmt.Errorf("expected quoted string, got %q", value)
	}
	out, err := strconv.Unquote(value)
	if err != nil {
		return "", err
	}
	return out, nil
}

func parseTOMLStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected string array, got %q", value)
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if body == "" {
		return nil, nil
	}
	parts := splitTOMLArray(body)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		v, err := parseTOMLString(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func splitTOMLArray(s string) []string {
	var out []string
	start := 0
	inString := false
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == ',' && !inString {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func stripTOMLComment(s string) string {
	inString := false
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == '#' && !inString {
			return s[:i]
		}
	}
	return s
}
