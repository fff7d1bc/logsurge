//go:build !unix

package main

import (
	"os"
	"path/filepath"
)

type dirLock struct {
	file *os.File
}

func lockDirectory(dir string) (*dirLock, error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	return &dirLock{file: f}, nil
}

func (l *dirLock) Close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
