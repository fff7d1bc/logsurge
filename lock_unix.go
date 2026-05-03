//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type dirLock struct {
	file *os.File
}

func lockDirectory(dir string) (*dirLock, error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// Nonblocking lock: a second logsurge writing the same directory could race
	// rotation/retention and corrupt the current-file contract.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("directory sink lock held: %w", err)
	}
	return &dirLock{file: f}, nil
}

func (l *dirLock) Close() error {
	var err error
	if l.file != nil {
		if unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); unlockErr != nil && err == nil {
			err = unlockErr
		}
		if closeErr := l.file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		l.file = nil
	}
	return err
}
