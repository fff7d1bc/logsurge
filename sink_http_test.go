package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPSinkBatchesRecords(t *testing.T) {
	var bodies []string
	var contentTypes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, string(body))
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
		BatchRecords: 2,
		Timeout:      time.Second,
	})
	for i := 0; i < 3; i++ {
		if err := sink.WriteRecord(Record{Time: time.Unix(int64(i), 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d bodies=%q", len(bodies), bodies)
	}
	if contentTypes[0] != "application/x-ndjson" {
		t.Fatalf("content type = %q", contentTypes[0])
	}
	if strings.Count(bodies[0], "\n") != 2 || strings.Count(bodies[1], "\n") != 1 {
		t.Fatalf("bodies = %#v", bodies)
	}
}

func TestHTTPSinkBatchesEncodedBytes(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
		BatchRecords: 100,
		BatchBytes:   160,
		Timeout:      time.Second,
	})
	line := []byte(`{"nested":"quote \" slash \\ newline \n"}`)
	for i := 0; i < 3; i++ {
		if err := sink.WriteRecord(Record{Time: time.Unix(int64(i), 0), End: RecordEndNewline, Line: line}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if len(bodies) < 2 {
		t.Fatalf("expected byte limit to split batches, bodies=%#v", bodies)
	}
	for _, body := range bodies {
		if strings.Count(body, "\n") > 2 {
			t.Fatalf("batch too large: %q", body)
		}
	}
}

func TestHTTPSinkSendsGELFRecords(t *testing.T) {
	var body string
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		body = string(data)
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := NewHTTPSink(server.URL, GELFHTTPEncoder{Host: "host-a"}, HTTPSinkConfig{
		BatchRecords: 1,
		Timeout:      time.Second,
	})
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if contentType != "application/x-ndjson" {
		t.Fatalf("content type = %q", contentType)
	}
	if !strings.Contains(body, `"version":"1.1"`) || !strings.Contains(body, `"host":"host-a"`) || !strings.Contains(body, `"short_message":"x"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestHTTPSinkRetriesNonSuccess(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
		BatchRecords: 1,
		Timeout:      time.Second,
		Retries:      1,
	})
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestHTTPSinkBearerAuth(t *testing.T) {
	var authHeaders []string
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		if attempts == 1 {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
		BatchRecords: 1,
		Timeout:      time.Second,
		Retries:      1,
		Auth:         HTTPAuthBearer,
		AuthSecret:   "token-value",
	})
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if len(authHeaders) != 2 {
		t.Fatalf("auth headers = %#v", authHeaders)
	}
	for _, header := range authHeaders {
		if header != "Bearer token-value" {
			t.Fatalf("auth header = %q", header)
		}
	}
}

func TestHTTPSinkBasicAuth(t *testing.T) {
	var usernames []string
	var passwords []string
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("missing basic auth")
		}
		usernames = append(usernames, username)
		passwords = append(passwords, password)
		if attempts == 1 {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
		BatchRecords: 1,
		Timeout:      time.Second,
		Retries:      1,
		Auth:         HTTPAuthBasic,
		AuthUsername: "log-user",
		AuthSecret:   "password-value",
	})
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if len(usernames) != 2 || len(passwords) != 2 {
		t.Fatalf("basic auth attempts = usernames=%#v passwords=%#v", usernames, passwords)
	}
	for i := range usernames {
		if usernames[i] != "log-user" || passwords[i] != "password-value" {
			t.Fatalf("basic auth = %q/%q", usernames[i], passwords[i])
		}
	}
}

func TestHTTPSinkNoAuthByDefault(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
		BatchRecords: 1,
		Timeout:      time.Second,
	})
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if authHeader != "" {
		t.Fatalf("auth header = %q", authHeader)
	}
}

func TestReadHTTPAuthSecretFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("token-value\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret, err := readHTTPAuthSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "token-value" {
		t.Fatalf("secret = %q", secret)
	}

	emptyPath := filepath.Join(dir, "empty")
	if err := os.WriteFile(emptyPath, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readHTTPAuthSecretFile(emptyPath); err == nil {
		t.Fatal("expected empty secret error")
	}

	username, password, err := parseHTTPBasicAuthSecret(httpAuthSecretSource(path), "log-user:password:value")
	if err != nil {
		t.Fatal(err)
	}
	if username != "log-user" || password != "password:value" {
		t.Fatalf("basic secret = %q/%q", username, password)
	}
	for _, secret := range []string{"missing-colon", ":password", "user:"} {
		if _, _, err := parseHTTPBasicAuthSecret(httpAuthSecretSource(path), secret); err == nil {
			t.Fatalf("expected basic secret error for %q", secret)
		}
	}
}

func TestNewSinkLoadsHTTPAuthSecret(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretPath, []byte("sink-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Output = OutputHTTP
	cfg.OutputTarget = server.URL
	cfg.HTTPAuth = HTTPAuthBearer
	cfg.HTTPAuthSecretFile = secretPath
	cfg.HTTPBatchRecords = 1
	sink, err := NewSink(cfg, io.Discard, PlainFormatter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer sink-token" {
		t.Fatalf("auth header = %q", authHeader)
	}

	cfg.HTTPAuthSecretFile = filepath.Join(dir, "missing")
	_, err = NewSink(cfg, io.Discard, PlainFormatter{})
	if err == nil {
		t.Fatal("expected missing secret error")
	}
	if !strings.Contains(err.Error(), cfg.HTTPAuthSecretFile) {
		t.Fatalf("error does not include path: %v", err)
	}
}

func TestNewSinkLoadsHTTPBasicAuthSecret(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "password")
	if err := os.WriteFile(secretPath, []byte("sink-user:sink-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var username string
	var password string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, _ = r.BasicAuth()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Output = OutputHTTP
	cfg.OutputTarget = server.URL
	cfg.HTTPAuth = HTTPAuthBasic
	cfg.HTTPAuthSecretFile = secretPath
	cfg.HTTPBatchRecords = 1
	sink, err := NewSink(cfg, io.Discard, PlainFormatter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if username != "sink-user" || password != "sink-password" {
		t.Fatalf("basic auth = %q/%q", username, password)
	}
}

func TestNewSinkLoadsHTTPAuthSecretFromEnv(t *testing.T) {
	t.Setenv(httpAuthSecretEnv, "env-token")
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Output = OutputHTTP
	cfg.OutputTarget = server.URL
	cfg.HTTPAuth = HTTPAuthBearer
	cfg.HTTPBatchRecords = 1
	sink, err := NewSink(cfg, io.Discard, PlainFormatter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer env-token" {
		t.Fatalf("auth header = %q", authHeader)
	}
}

func TestNewSinkLoadsHTTPBasicAuthSecretFromEnv(t *testing.T) {
	t.Setenv(httpAuthSecretEnv, "env-user:env-password")
	var username string
	var password string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, _ = r.BasicAuth()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Output = OutputHTTP
	cfg.OutputTarget = server.URL
	cfg.HTTPAuth = HTTPAuthBasic
	cfg.HTTPBatchRecords = 1
	sink, err := NewSink(cfg, io.Discard, PlainFormatter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if username != "env-user" || password != "env-password" {
		t.Fatalf("basic auth = %q/%q", username, password)
	}
}

func TestNewSinkHTTPAuthEmptyEnvMeansNoAuth(t *testing.T) {
	t.Setenv(httpAuthSecretEnv, "")
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Output = OutputHTTP
	cfg.OutputTarget = server.URL
	cfg.HTTPAuth = HTTPAuthBearer
	cfg.HTTPBatchRecords = 1
	sink, err := NewSink(cfg, io.Discard, PlainFormatter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if authHeader != "" {
		t.Fatalf("auth header = %q", authHeader)
	}
}

func TestNewSinkHTTPAuthFileWinsOverEnv(t *testing.T) {
	t.Setenv(httpAuthSecretEnv, "env-token")
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Output = OutputHTTP
	cfg.OutputTarget = server.URL
	cfg.HTTPAuth = HTTPAuthBearer
	cfg.HTTPAuthSecretFile = secretPath
	cfg.HTTPBatchRecords = 1
	sink, err := NewSink(cfg, io.Discard, PlainFormatter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer file-token" {
		t.Fatalf("auth header = %q", authHeader)
	}
}

func TestNewSinkRejectsMalformedBasicAuthEnv(t *testing.T) {
	t.Setenv(httpAuthSecretEnv, "missing-colon")
	cfg := DefaultConfig()
	cfg.Output = OutputHTTP
	cfg.OutputTarget = "https://example.test/logs"
	cfg.HTTPAuth = HTTPAuthBasic
	if _, err := NewSink(cfg, io.Discard, PlainFormatter{}); err == nil {
		t.Fatal("expected malformed basic auth env error")
	}
}

func TestHTTPSinkWarnsAndContinuesOnAuthStatus(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					http.Error(w, "auth failed", status)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			var diagnostics strings.Builder
			sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
				BatchRecords: 1,
				Timeout:      time.Second,
				Diagnostics:  &diagnostics,
			})

			if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("dropped")}); err != nil {
				t.Fatal(err)
			}
			if err := sink.WriteRecord(Record{Time: time.Unix(1, 0), End: RecordEndNewline, Line: []byte("sent")}); err != nil {
				t.Fatal(err)
			}
			if requests != 2 {
				t.Fatalf("requests = %d", requests)
			}
			got := diagnostics.String()
			if !strings.Contains(got, http.StatusText(status)) || !strings.Contains(got, "dropped 1 records") {
				t.Fatalf("diagnostics = %q", got)
			}
		})
	}
}

func TestHTTPSinkKeepsServerErrorFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer server.Close()
	sink := NewHTTPSink(server.URL, JSONLineHTTPEncoder{}, HTTPSinkConfig{
		BatchRecords: 1,
		Timeout:      time.Second,
	})
	if err := sink.WriteRecord(Record{Time: time.Unix(0, 0), End: RecordEndNewline, Line: []byte("x")}); err == nil {
		t.Fatal("expected server error")
	}
}
