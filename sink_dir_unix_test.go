//go:build unix

package main

import "testing"

func TestDirectorySinkLockRejectsSecondWriter(t *testing.T) {
	dir := t.TempDir()
	first, err := NewDirectorySink(dir, PlainFormatter{}, DirectorySinkConfig{
		MaxBytes: 1000,
		MaxFiles: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewDirectorySink(dir, PlainFormatter{}, DirectorySinkConfig{
		MaxBytes: 1000,
		MaxFiles: 10,
	})
	if err == nil {
		_ = second.Close()
		t.Fatal("expected lock error")
	}
}
