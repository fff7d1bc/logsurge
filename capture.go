package main

import (
	"io"
	"os"
	"os/exec"
)

type Capture struct {
	Reader *os.File
	writer *os.File
}

func NewCapture(cmd *exec.Cmd) (*Capture, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// stdout and stderr intentionally share one pipe. Two separate pipes would
	// make global ordering a race between reader goroutines; the shared pipe
	// preserves the kernel-visible order of writes that reached this fd.
	cmd.Stdout = w
	cmd.Stderr = w
	return &Capture{Reader: r, writer: w}, nil
}

func (c *Capture) CloseParentWriter() error {
	if c.writer == nil {
		return nil
	}
	err := c.writer.Close()
	c.writer = nil
	return err
}

func (c *Capture) Close() error {
	var err error
	if c.writer != nil {
		err = c.writer.Close()
		c.writer = nil
	}
	if c.Reader != nil {
		if e := c.Reader.Close(); err == nil {
			err = e
		}
		c.Reader = nil
	}
	return err
}

type chunkResult struct {
	data    []byte
	buf     []byte
	release chan<- []byte
	err     error
}

func StartChunkReader(r io.Reader, chunkSize int) <-chan chunkResult {
	const channelSize = 16
	ch := make(chan chunkResult, channelSize)
	free := make(chan []byte, channelSize+1)
	for i := 0; i < cap(free); i++ {
		free <- make([]byte, chunkSize)
	}
	go func() {
		defer close(ch)
		for {
			buf := <-free
			n, err := r.Read(buf)
			if n > 0 {
				// Ownership of buf moves to the receiver. The framer releases it
				// after copying data into its own pending-line buffer.
				ch <- chunkResult{
					data:    buf[:n],
					buf:     buf,
					release: free,
				}
			} else {
				free <- buf
			}
			if err != nil {
				if err != io.EOF {
					ch <- chunkResult{err: err}
				}
				return
			}
		}
	}()
	return ch
}
