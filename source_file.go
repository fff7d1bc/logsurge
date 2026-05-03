package main

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

type followFileReader struct {
	path       string
	startAtEnd bool
	poll       time.Duration

	mu     sync.Mutex
	done   chan struct{}
	closed bool
	file   *os.File
	info   os.FileInfo
	offset int64
	opened bool
}

func NewFollowFileReader(path string, start FileStart, poll time.Duration) *followFileReader {
	startAtEnd := false
	if start == FileStartEnd {
		// "start at end" only skips bytes that already exist when the reader is
		// created. If the file appears later, read it from byte zero so a newly
		// created log is not silently skipped.
		if _, err := os.Stat(path); err == nil {
			startAtEnd = true
		}
	}
	return &followFileReader{
		path:       path,
		startAtEnd: startAtEnd,
		poll:       poll,
		done:       make(chan struct{}),
	}
}

func (r *followFileReader) Read(p []byte) (int, error) {
	for {
		if err := r.ensureOpen(); err != nil {
			return 0, err
		}
		if r.isClosed() {
			return 0, io.EOF
		}
		file := r.currentFile()
		if file == nil {
			if err := r.wait(); err != nil {
				return 0, err
			}
			continue
		}
		n, err := file.Read(p)
		if n > 0 {
			r.addOffset(int64(n))
			return n, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		// EOF is only idle state for a followed file. The path may have rotated
		// or the same inode may have truncated, so check before sleeping.
		if err := r.handleEOF(); err != nil {
			return 0, err
		}
		if err := r.wait(); err != nil {
			return 0, err
		}
	}
}

func (r *followFileReader) Close() error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.done)
	}
	file := r.file
	r.file = nil
	r.mu.Unlock()
	if file != nil {
		return file.Close()
	}
	return nil
}

func (r *followFileReader) ensureOpen() error {
	r.mu.Lock()
	if r.closed || r.file != nil {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	file, info, err := openFollowFile(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A watched file may not exist yet at startup or after rotation.
			// Missing-by-path is a normal tail -F state, not a fatal error.
			return nil
		}
		return err
	}

	offset := int64(0)
	if !r.opened && r.startAtEnd {
		offset = info.Size()
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			_ = file.Close()
			return err
		}
	}

	r.mu.Lock()
	if r.closed {
		// Close can win while os.Open is in progress. Do not publish the new
		// descriptor after shutdown has started.
		r.mu.Unlock()
		_ = file.Close()
		return nil
	}
	if r.file == nil {
		r.file = file
		r.info = info
		r.offset = offset
		r.opened = true
		file = nil
	}
	r.mu.Unlock()
	if file != nil {
		_ = file.Close()
	}
	return nil
}

func (r *followFileReader) handleEOF() error {
	info, err := os.Stat(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.file == nil {
		return nil
	}
	if os.SameFile(r.info, info) {
		if info.Size() < r.offset {
			// Same inode got truncated in place. Treat that as a new stream from
			// byte zero rather than waiting forever at the old offset.
			if _, err := r.file.Seek(0, io.SeekStart); err != nil {
				return err
			}
			r.offset = 0
			r.info = info
		}
		return nil
	}
	// The path now points at a different file. We only switch after reaching
	// EOF on the old descriptor, which drains the remainder of a renamed file
	// before following the replacement at the same path. Switching earlier can
	// lose rollover bytes still readable through the old fd.
	// If the rotated path is later unlinked, the open fd still keeps the inode
	// readable on Unix-like systems; copytruncate is different because it
	// destroys unread bytes in place.
	_ = r.file.Close()
	r.file = nil
	r.info = nil
	r.offset = 0
	return nil
}

func (r *followFileReader) wait() error {
	timer := time.NewTimer(r.poll)
	defer timer.Stop()
	select {
	case <-r.done:
		return io.EOF
	case <-timer.C:
		return nil
	}
}

func (r *followFileReader) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *followFileReader) currentFile() *os.File {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file
}

func (r *followFileReader) addOffset(n int64) {
	r.mu.Lock()
	r.offset += n
	r.mu.Unlock()
}

func openFollowFile(path string) (*os.File, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}
