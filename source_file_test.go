package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFollowFileWaitsForMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	r := NewFollowFileReader(path, FileStartEnd, 5*time.Millisecond)
	defer r.Close()
	done := readOnce(r, 64)
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, []byte("created\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := waitRead(t, done)
	if got != "created\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFollowFileStartEndSkipsExistingBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFollowFileReader(path, FileStartEnd, 5*time.Millisecond)
	defer r.Close()
	done := readOnce(r, 64)
	time.Sleep(20 * time.Millisecond)
	appendFile(t, path, "new\n")
	got := waitRead(t, done)
	if got != "new\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFollowFileStartBeginningReadsExistingBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFollowFileReader(path, FileStartBeginning, 5*time.Millisecond)
	defer r.Close()
	got := waitRead(t, readOnce(r, 64))
	if got != "old\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFollowFileRotationDrainsOldFileThenSwitches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	rotated := filepath.Join(dir, "app.log.1")
	if err := os.WriteFile(path, []byte("old1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFollowFileReader(path, FileStartBeginning, 5*time.Millisecond)
	defer r.Close()
	if got := waitRead(t, readOnce(r, 64)); got != "old1\n" {
		t.Fatalf("first read = %q", got)
	}
	if err := os.Rename(path, rotated); err != nil {
		t.Fatal(err)
	}
	appendFile(t, rotated, "old2\n")
	if runtime.GOOS != "windows" {
		if err := os.Remove(rotated); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := waitRead(t, readOnce(r, 64)); got != "old2\n" {
		t.Fatalf("old file read = %q", got)
	}
	if got := waitRead(t, readOnce(r, 64)); got != "new\n" {
		t.Fatalf("new file read = %q", got)
	}
}

func TestFollowFileTruncationSeeksToBeginning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFollowFileReader(path, FileStartBeginning, 5*time.Millisecond)
	defer r.Close()
	if got := waitRead(t, readOnce(r, 64)); got != "before\n" {
		t.Fatalf("first read = %q", got)
	}
	if err := os.WriteFile(path, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := waitRead(t, readOnce(r, 64)); got != "after\n" {
		t.Fatalf("after truncate read = %q", got)
	}
}

type readResult struct {
	data string
	err  error
}

func readOnce(r io.Reader, size int) <-chan readResult {
	ch := make(chan readResult, 1)
	go func() {
		buf := make([]byte, size)
		n, err := r.Read(buf)
		ch <- readResult{data: string(buf[:n]), err: err}
	}()
	return ch
}

func waitRead(t *testing.T, ch <-chan readResult) string {
	t.Helper()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read error: %v", res.err)
		}
		return res.data
	case <-time.After(time.Second):
		t.Fatal("read timed out")
		return ""
	}
}

func appendFile(t *testing.T, path string, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
