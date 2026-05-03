package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type DirectorySinkConfig struct {
	MaxBytes int
	MaxFiles int
}

type DirectorySink struct {
	dir       string
	formatter Formatter
	cfg       DirectorySinkConfig
	lock      *dirLock
	file      *os.File
	w         *bufio.Writer
	buf       bytes.Buffer
	size      int64
}

func NewDirectorySink(dir string, formatter Formatter, cfg DirectorySinkConfig) (*DirectorySink, error) {
	if cfg.MaxBytes <= 0 {
		return nil, fmt.Errorf("directory sink max bytes must be greater than zero")
	}
	if cfg.MaxFiles < 0 {
		return nil, fmt.Errorf("directory sink max files must not be negative")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lock, err := lockDirectory(dir)
	if err != nil {
		return nil, err
	}
	s := &DirectorySink{
		dir:       dir,
		formatter: formatter,
		cfg:       cfg,
		lock:      lock,
	}
	if err := s.openCurrent(); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return s, nil
}

func (s *DirectorySink) WriteRecord(record Record) error {
	s.buf.Reset()
	if err := s.formatter.Format(record, &s.buf); err != nil {
		return err
	}
	// Rotate only between formatted records. This can let one very large record
	// exceed MaxBytes, but it avoids splitting a single JSON/plain record across
	// files.
	if s.size > 0 && s.size+int64(s.buf.Len()) > int64(s.cfg.MaxBytes) {
		if err := s.rotate(); err != nil {
			return err
		}
	}
	n, err := s.w.Write(s.buf.Bytes())
	s.size += int64(n)
	return err
}

func (s *DirectorySink) Flush() error {
	if s.w == nil {
		return nil
	}
	return s.w.Flush()
}

func (s *DirectorySink) Close() error {
	var err error
	if s.w != nil {
		if flushErr := s.w.Flush(); flushErr != nil && err == nil {
			err = flushErr
		}
	}
	if s.file != nil {
		if syncErr := s.file.Sync(); syncErr != nil && err == nil {
			err = syncErr
		}
		if closeErr := s.file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		s.file = nil
		s.w = nil
	}
	if s.lock != nil {
		if lockErr := s.lock.Close(); lockErr != nil && err == nil {
			err = lockErr
		}
		s.lock = nil
	}
	return err
}

func (s *DirectorySink) openCurrent() error {
	path := filepath.Join(s.dir, "current")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	s.file = f
	s.w = bufio.NewWriter(f)
	s.size = info.Size()
	return nil
}

func (s *DirectorySink) rotate() error {
	// Flush/fsync before rename so a rotated file is durable as a complete
	// segment. Then fsync the directory so the rename itself is durable on
	// filesystems that require it.
	if err := s.w.Flush(); err != nil {
		return err
	}
	if err := s.file.Sync(); err != nil {
		return err
	}
	if err := s.file.Close(); err != nil {
		return err
	}
	s.file = nil
	s.w = nil

	current := filepath.Join(s.dir, "current")
	target, err := s.rotationPath()
	if err != nil {
		return err
	}
	if err := os.Rename(current, target); err != nil {
		return err
	}
	if err := syncDir(s.dir); err != nil {
		return err
	}
	if err := s.enforceRetention(); err != nil {
		return err
	}
	return s.openCurrent()
}

func (s *DirectorySink) rotationPath() (string, error) {
	base := time.Now().UTC().Format("20060102T150405.000000000Z") + ".log"
	for i := 0; i < 1000; i++ {
		name := base
		if i > 0 {
			// Use "~NNN" so collision names sort after the base timestamp while
			// still matching the rotated log retention pattern.
			name = strings.TrimSuffix(base, ".log") + fmt.Sprintf("~%03d.log", i)
		}
		path := filepath.Join(s.dir, name)
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate rotation filename")
}

func (s *DirectorySink) enforceRetention() error {
	if s.cfg.MaxFiles == 0 {
		return nil
	}
	files, err := rotatedFiles(s.dir)
	if err != nil {
		return err
	}
	for len(files) > s.cfg.MaxFiles {
		if err := os.Remove(filepath.Join(s.dir, files[0])); err != nil && !os.IsNotExist(err) {
			return err
		}
		files = files[1:]
	}
	if err := syncDir(s.dir); err != nil {
		return err
	}
	return nil
}

func rotatedFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		// Retention owns only completed rotated segments. Do not consider
		// current, lock, temporary files, or unrelated files in the directory.
		if entry.Type().IsRegular() && isRotationName(entry.Name()) {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func isRotationName(name string) bool {
	if !strings.HasSuffix(name, ".log") {
		return false
	}
	stem := strings.TrimSuffix(name, ".log")
	if len(stem) == len("20060102T150405.000000000Z")+4 {
		if stem[len(stem)-4] != '~' {
			return false
		}
		if !allDigits(stem[len(stem)-3:]) {
			return false
		}
		stem = stem[:len(stem)-4]
	}
	if len(stem) != len("20060102T150405.000000000Z") {
		return false
	}
	return allDigits(stem[:8]) &&
		stem[8] == 'T' &&
		allDigits(stem[9:15]) &&
		stem[15] == '.' &&
		allDigits(stem[16:25]) &&
		stem[25] == 'Z'
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func syncDir(dir string) error {
	// Directory fsync is meaningful on Unix filesystems after rename/unlink.
	// Some platforms may implement it as a cheap no-op; keep the call local to
	// directory sink durability code.
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
