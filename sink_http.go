package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const httpAuthSecretEnv = "LOGSURGE_HTTP_AUTH_SECRET"

type HTTPSinkConfig struct {
	BatchRecords int
	BatchBytes   int
	Timeout      time.Duration
	Retries      int
	Auth         HTTPAuthMode
	AuthUsername string
	AuthSecret   string
	Diagnostics  io.Writer
}

type HTTPSink struct {
	url     string
	encoder HTTPRecordEncoder
	cfg     HTTPSinkConfig
	client  *http.Client
	buf     bytes.Buffer
	records int
}

func NewHTTPSink(url string, encoder HTTPRecordEncoder, cfg HTTPSinkConfig) *HTTPSink {
	if cfg.BatchBytes <= 0 {
		cfg.BatchBytes = DefaultConfig().HTTPBatchBytes
	}
	return &HTTPSink{
		url:     url,
		encoder: encoder,
		cfg:     cfg,
		client:  &http.Client{Timeout: cfg.Timeout},
	}
}

func (s *HTTPSink) WriteRecord(record Record) error {
	if err := s.encoder.Encode(record, &s.buf); err != nil {
		return err
	}
	s.records++
	// The queue is bounded by raw record bytes, but JSON escaping can expand
	// pathological input significantly. Keep HTTP batches bounded by encoded
	// bytes as well as record count so retries do not copy very large payloads
	// just because many small-but-expensive records fit the record limit.
	if s.records >= s.cfg.BatchRecords || s.buf.Len() >= s.cfg.BatchBytes {
		return s.Flush()
	}
	return nil
}

func (s *HTTPSink) Flush() error {
	if s.records == 0 {
		return nil
	}
	// Copy the payload before retrying. The sink buffer is only cleared after a
	// successful 2xx response, so failed batches remain available for bounded
	// retries and final Close.
	payload := append([]byte(nil), s.buf.Bytes()...)
	var lastErr error
	var lastStatus int
	var lastStatusText string
	for attempt := 0; attempt <= s.cfg.Retries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", s.encoder.ContentType())
		switch s.cfg.Auth {
		case HTTPAuthBearer:
			req.Header.Set("Authorization", "Bearer "+s.cfg.AuthSecret)
		case HTTPAuthBasic:
			req.SetBasicAuth(s.cfg.AuthUsername, s.cfg.AuthSecret)
		}
		resp, err := s.client.Do(req)
		if err == nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				_ = resp.Body.Close()
				s.buf.Reset()
				s.records = 0
				return nil
			}
			lastStatus = resp.StatusCode
			lastStatusText = resp.Status
			lastErr = fmt.Errorf("HTTP sink status %s", resp.Status)
			_ = resp.Body.Close()
		} else {
			lastErr = err
			lastStatus = 0
			lastStatusText = ""
		}
		if attempt < s.cfg.Retries {
			// Simple bounded backoff keeps this sink stdlib-only and predictable.
			// A long remote outage is still treated as sink failure, not as a
			// durable offline spool.
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	if lastStatus == http.StatusUnauthorized || lastStatus == http.StatusForbidden {
		// Auth-denial statuses are nonfatal by product choice: receiver policy
		// should not kill an ad-hoc wrapped command. Drop this batch, warn, and
		// let later batches try again.
		s.warnNonfatalAuthStatus(lastStatusText)
		s.buf.Reset()
		s.records = 0
		return nil
	}
	return lastErr
}

func (s *HTTPSink) Close() error {
	return s.Flush()
}

func readHTTPAuthSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read HTTP auth secret file %q: %w", path, err)
	}
	// Operators commonly create token files with a trailing newline. Strip only
	// line endings; spaces and tabs may be intentional secret bytes.
	secret := strings.TrimRight(string(data), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("HTTP auth secret file %q is empty", path)
	}
	return secret, nil
}

func resolveHTTPAuthSecret(path string) (string, error) {
	if path != "" {
		return readHTTPAuthSecretFile(path)
	}
	return os.Getenv(httpAuthSecretEnv), nil
}

func httpAuthSecretSource(path string) string {
	if path != "" {
		return fmt.Sprintf("HTTP basic auth secret file %q", path)
	}
	return "HTTP basic auth environment variable " + httpAuthSecretEnv
}

func parseHTTPBasicAuthSecret(source string, secret string) (string, string, error) {
	// Files and env use the same user:password shape. Split once so passwords
	// may contain ':' while still requiring a username and non-empty password.
	username, password, ok := strings.Cut(secret, ":")
	if !ok || username == "" || password == "" {
		return "", "", fmt.Errorf("%s must contain user:password", source)
	}
	return username, password, nil
}

func (s *HTTPSink) warnNonfatalAuthStatus(status string) {
	if s.cfg.Diagnostics == nil {
		return
	}
	fmt.Fprintf(s.cfg.Diagnostics, "logsurge: HTTP sink status %s from %s; dropped %d records and continuing\n", status, s.url, s.records)
}
