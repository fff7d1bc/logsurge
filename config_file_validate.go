package main

import (
	"errors"
	"fmt"
)

func validateConfigFile(cfg Config) (Config, error) {
	if len(cfg.Inputs) == 0 {
		return cfg, errors.New("config must contain at least one [[inputs]] section")
	}
	if err := finalizeOutputs(&cfg); err != nil {
		return cfg, err
	}
	if err := validateOutput(cfg); err != nil {
		return cfg, err
	}
	if cfg.FlushInterval < 0 {
		return cfg, errors.New("durations must not be negative")
	}
	if cfg.HealthListen != "" {
		if err := validateLoopbackAddress(cfg.HealthListen); err != nil {
			return cfg, fmt.Errorf("invalid health_listen: %w", err)
		}
	}
	for i := range cfg.Inputs {
		if err := normalizeInputConfig(&cfg, i); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func normalizeInputConfig(cfg *Config, i int) error {
	input := &cfg.Inputs[i]
	applyInputDefaults(cfg, input)
	if input.Kind == "" {
		switch {
		case input.Path != "":
			input.Kind = InputKindFile
		case input.Listen != "":
			return fmt.Errorf("input %d kind is required for network input", i)
		case input.Directory != "" || input.CursorFile != "":
			input.Kind = InputKindJournal
		default:
			return fmt.Errorf("input %d kind is required", i)
		}
	}
	switch input.Kind {
	case InputKindFile:
		if err := normalizeFileInput(input, i); err != nil {
			return err
		}
	case InputKindJournal:
		if err := normalizeJournalInput(input, i); err != nil {
			return err
		}
	case InputKindTCP, InputKindUDP:
		if err := normalizeNetworkInput(cfg, input, i); err != nil {
			return err
		}
	default:
		return fmt.Errorf("input %d unsupported kind %q", i, input.Kind)
	}
	return validateCommonInputConfig(input, i)
}

func applyInputDefaults(cfg *Config, input *InputConfig) {
	if !input.ANSISet {
		input.ANSI = cfg.ANSI
	}
	if !input.QueueRecordsSet {
		input.QueueRecords = cfg.QueueRecords
	}
	if !input.QueueBytesSet {
		input.QueueBytes = cfg.QueueBytes
	}
	if !input.OverflowSet {
		input.Overflow = cfg.Overflow
	}
	if !input.MaxFragmentBytesSet {
		input.MaxFragmentBytes = cfg.MaxFragmentBytes
	}
	if !input.PartialFlushSet {
		input.PartialFlushInterval = cfg.PartialFlushInterval
	}
	if !input.FileStartSet {
		input.FileStart = cfg.FileStart
	}
	if !input.FilePollIntervalSet {
		input.FilePollInterval = cfg.FilePollInterval
	}
}

func normalizeFileInput(input *InputConfig, i int) error {
	if input.Path == "" {
		return fmt.Errorf("input %d path is required", i)
	}
	if input.Listen != "" || input.MaxConnectionsSet {
		return fmt.Errorf("input %d network keys require kind = \"tcp\" or kind = \"udp\"", i)
	}
	if input.Directory != "" || input.CursorFile != "" || input.JournalStart != "" && input.JournalStart != JournalStartEnd {
		return fmt.Errorf("input %d journal keys require kind = \"journal\"", i)
	}
	if input.Source == "" {
		input.Source = input.Path
	}
	return nil
}

func normalizeJournalInput(input *InputConfig, i int) error {
	if input.Directory == "" {
		input.Directory = "/var/log/journal"
	}
	if input.Path != "" {
		return fmt.Errorf("input %d path is only valid for file inputs", i)
	}
	if input.Listen != "" || input.MaxConnectionsSet {
		return fmt.Errorf("input %d network keys require kind = \"tcp\" or kind = \"udp\"", i)
	}
	if input.JournalStart == "" {
		input.JournalStart = JournalStartEnd
	}
	if input.JournalStart != JournalStartEnd && input.JournalStart != JournalStartAll {
		return fmt.Errorf("input %d unsupported journal start %q", i, input.JournalStart)
	}
	if input.Source == "" {
		input.Source = "journald"
	}
	return nil
}

func normalizeNetworkInput(cfg *Config, input *InputConfig, i int) error {
	if input.Listen == "" {
		return fmt.Errorf("input %d listen is required", i)
	}
	if input.Path != "" || input.Directory != "" || input.CursorFile != "" || input.JournalStart != "" && input.JournalStart != JournalStartEnd {
		return fmt.Errorf("input %d file/journal keys are not valid for network inputs", i)
	}
	if err := validateLoopbackAddress(input.Listen); err != nil {
		return fmt.Errorf("input %d invalid listen: %w", i, err)
	}
	if input.Kind == InputKindTCP {
		if input.MaxConnectionsSet && input.MaxConnections <= 0 {
			return fmt.Errorf("input %d max_connections must be greater than zero", i)
		}
		if input.MaxConnections <= 0 {
			input.MaxConnections = cfg.ListenMaxConnections
		}
	}
	if input.Kind == InputKindUDP && input.MaxConnectionsSet {
		return fmt.Errorf("input %d max_connections is only valid for tcp inputs", i)
	}
	if input.Source == "" {
		input.Source = string(input.Kind) + "://" + input.Listen
	}
	return nil
}

func validateCommonInputConfig(input *InputConfig, i int) error {
	if input.QueueRecords <= 0 {
		return fmt.Errorf("input %d queue_records must be greater than zero", i)
	}
	if input.QueueBytes <= 0 {
		return fmt.Errorf("input %d queue_bytes must be greater than zero", i)
	}
	if input.MaxFragmentBytes <= 0 {
		return fmt.Errorf("input %d max_fragment_bytes must be greater than zero", i)
	}
	if input.MaxFragmentBytes > input.QueueBytes {
		return fmt.Errorf("input %d max_fragment_bytes must not exceed queue_bytes", i)
	}
	if input.Overflow == OverflowBlock {
		return fmt.Errorf("input %d overflow block is not supported in daemon mode", i)
	}
	if input.Overflow != OverflowDropOldest && input.Overflow != OverflowDropNewest {
		return fmt.Errorf("input %d unsupported overflow %q", i, input.Overflow)
	}
	if input.ANSI != ANSIStrip && input.ANSI != ANSIKeep {
		return fmt.Errorf("input %d unsupported ANSI mode %q", i, input.ANSI)
	}
	if input.PartialFlushInterval < 0 || input.FilePollInterval <= 0 {
		return fmt.Errorf("input %d invalid file timing", i)
	}
	return nil
}

func parseConfigOverflow(value string) (OverflowPolicy, error) {
	switch OverflowPolicy(value) {
	case OverflowDropOldest, OverflowDropNewest:
		return OverflowPolicy(value), nil
	case OverflowBlock:
		return "", errors.New("overflow block is not supported in daemon mode")
	default:
		return "", fmt.Errorf("unsupported overflow %q", value)
	}
}
