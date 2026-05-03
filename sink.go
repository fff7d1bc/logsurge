package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
)

type Sink interface {
	WriteRecord(record Record) error
	Flush() error
	Close() error
}

func NewSink(cfg Config, stdout io.Writer, formatter Formatter) (Sink, error) {
	return NewSinkWithDiagnostics(cfg, stdout, io.Discard, formatter)
}

func NewSinkWithDiagnostics(cfg Config, stdout io.Writer, diagnostics io.Writer, formatter Formatter) (Sink, error) {
	if diagnostics == nil {
		diagnostics = os.Stderr
	}
	outputs := normalizedOutputs(cfg)
	sinks := make([]Sink, 0, len(outputs))
	for _, output := range outputs {
		sink, err := newSingleSink(cfg, output, stdout, diagnostics, formatter)
		if err != nil {
			for _, opened := range sinks {
				_ = opened.Close()
			}
			return nil, err
		}
		sinks = append(sinks, sink)
	}
	if len(sinks) == 1 {
		return sinks[0], nil
	}
	return fanoutSink{sinks: sinks}, nil
}

func newSingleSink(cfg Config, output OutputConfig, stdout io.Writer, diagnostics io.Writer, formatter Formatter) (Sink, error) {
	switch output.Kind {
	case OutputStdout:
		return NewStdoutSink(stdout, formatter), nil
	case OutputDir:
		return NewDirectorySink(output.Target, formatter, DirectorySinkConfig{
			MaxBytes: output.DirMaxBytes,
			MaxFiles: output.DirMaxFiles,
		})
	case OutputHTTP:
		u, err := url.Parse(output.Target)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid HTTP sink URL %q", output.Target)
		}
		var authUsername string
		var authSecret string
		authMode := output.HTTPAuth
		if authMode != HTTPAuthNone {
			// Explicit files win over env so daemon deployments can keep secrets
			// in root-owned paths. Empty env falls back to no auth because some
			// receivers are intentionally open in local/ad-hoc setups.
			authSecret, err = resolveHTTPAuthSecret(output.HTTPAuthSecretFile)
			if err != nil {
				return nil, err
			}
			if authSecret == "" {
				authMode = HTTPAuthNone
			} else if authMode == HTTPAuthBasic {
				authUsername, authSecret, err = parseHTTPBasicAuthSecret(httpAuthSecretSource(output.HTTPAuthSecretFile), authSecret)
				if err != nil {
					return nil, err
				}
			}
		}
		encoder, err := newHTTPRecordEncoder(output.HTTPFormat, cfg.MetadataFields)
		if err != nil {
			return nil, err
		}
		return NewHTTPSink(output.Target, encoder, HTTPSinkConfig{
			BatchRecords: output.HTTPBatchRecords,
			BatchBytes:   output.HTTPBatchBytes,
			Timeout:      output.HTTPTimeout,
			Retries:      output.HTTPRetries,
			Auth:         authMode,
			AuthUsername: authUsername,
			AuthSecret:   authSecret,
			Diagnostics:  diagnostics,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported output sink %q", output.Kind)
	}
}
