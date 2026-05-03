package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

var journalctlCommand = "journalctl"

type journalSource struct {
	input              InputConfig
	baseMetadata       map[string]any
	maxFragmentBytes   int
	terminationTimeout time.Duration

	mu     sync.Mutex
	cmd    *exec.Cmd
	done   chan struct{}
	closed bool
}

func newJournalSource(input InputConfig, baseMetadata map[string]any, maxFragmentBytes int, terminationTimeout time.Duration) *journalSource {
	return &journalSource{
		input:              input,
		baseMetadata:       baseMetadata,
		maxFragmentBytes:   maxFragmentBytes,
		terminationTimeout: terminationTimeout,
	}
}

func (s *journalSource) Start(q *Queue) (<-chan error, error) {
	args := journalctlArgs(s.input)
	cmd := exec.Command(journalctlCommand, args...)
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	s.mu.Lock()
	s.cmd = cmd
	s.done = done
	s.mu.Unlock()

	stderrDone := make(chan string, 1)
	go func() {
		stderrDone <- readLimitedString(stderr, 16*1024)
	}()

	out := make(chan error, 1)
	go func() {
		readErr := s.read(stdout, q)
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		var waitErr error
		if readErr != nil {
			requestTerminateProcess(cmd)
			waitErr = s.waitAfterReadError(cmd, waitDone)
		} else {
			waitErr = <-waitDone
		}
		close(done)
		q.Close()
		stderrText := <-stderrDone
		if s.isClosed() {
			out <- nil
			return
		}
		if readErr != nil {
			out <- readErr
			return
		}
		if waitErr != nil {
			if stderrText != "" {
				out <- fmt.Errorf("journalctl failed: %w: %s", waitErr, stderrText)
			} else {
				out <- fmt.Errorf("journalctl failed: %w", waitErr)
			}
			return
		}
		out <- nil
	}()
	return out, nil
}

func (s *journalSource) waitAfterReadError(cmd *exec.Cmd, waitDone <-chan error) error {
	timeout := s.terminationTimeout
	if timeout == 0 {
		forceKillProcess(cmd)
		return <-waitDone
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-waitDone:
		return err
	case <-timer.C:
		forceKillProcess(cmd)
		return <-waitDone
	}
}

func journalctlArgs(input InputConfig) []string {
	// Stay on the supported journalctl streaming interface instead of parsing
	// .journal files directly or linking libsystemd/cgo into this stdlib tool.
	args := []string{
		"--directory=" + input.Directory,
		"--follow",
		"--output=json",
		"--no-pager",
		"--quiet",
		"--output-fields=MESSAGE,__CURSOR,__REALTIME_TIMESTAMP,_BOOT_ID,_SYSTEMD_UNIT,_SYSTEMD_USER_UNIT,SYSLOG_IDENTIFIER,PRIORITY,_PID,_TRANSPORT",
	}
	if input.CursorFile != "" {
		args = append(args, "--cursor-file="+input.CursorFile)
	}
	switch input.JournalStart {
	case JournalStartAll:
		args = append(args, "--no-tail")
	default:
		args = append(args, "--lines=0")
	}
	return args
}

func (s *journalSource) read(r io.Reader, q *Queue) error {
	reader := bufio.NewReaderSize(r, 32*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if len(line) > 0 {
				if pushErr := s.pushJournalLine(q, line); pushErr != nil {
					return pushErr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (s *journalSource) pushJournalLine(q *Queue, line []byte) error {
	rec, err := parseJournalRecord(line, s.input.Source, s.baseMetadata)
	if err != nil {
		return err
	}
	if s.maxFragmentBytes <= 0 || len(rec.Line) <= s.maxFragmentBytes {
		q.Push(RecordMeta{
			Time:      rec.Time,
			End:       RecordEndNewline,
			Source:    rec.Source,
			Metadata:  rec.Metadata,
			Continued: false,
		}, rec.Line)
		return nil
	}
	remaining := rec.Line
	continued := false
	for len(remaining) > s.maxFragmentBytes {
		q.Push(RecordMeta{
			Time:      rec.Time,
			End:       RecordEndMaxBytes,
			Source:    rec.Source,
			Metadata:  rec.Metadata,
			Continued: continued,
		}, remaining[:s.maxFragmentBytes])
		remaining = remaining[s.maxFragmentBytes:]
		continued = true
	}
	q.Push(RecordMeta{
		Time:      rec.Time,
		End:       RecordEndNewline,
		Source:    rec.Source,
		Metadata:  rec.Metadata,
		Continued: continued,
	}, remaining)
	return nil
}

func parseJournalRecord(line []byte, source string, baseMetadata map[string]any) (Record, error) {
	var fields map[string]any
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	if err := dec.Decode(&fields); err != nil {
		return Record{}, fmt.Errorf("invalid journal JSON: %w", err)
	}
	metadata := copyMetadataMap(baseMetadata)
	message, ok := journalString(fields["MESSAGE"])
	if !ok {
		message = "[journal entry without MESSAGE]"
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata["journal_message_omitted"] = true
	}
	metadata = addJournalMetadata(metadata, fields)
	return Record{
		Time:     journalTimestamp(fields["__REALTIME_TIMESTAMP"]),
		Line:     []byte(message),
		End:      RecordEndNewline,
		Source:   source,
		Metadata: metadata,
	}, nil
}

func addJournalMetadata(metadata map[string]any, fields map[string]any) map[string]any {
	for journalKey, metadataKey := range map[string]string{
		"__CURSOR":           "journal_cursor",
		"_BOOT_ID":           "journal_boot_id",
		"_SYSTEMD_UNIT":      "journal_unit",
		"_SYSTEMD_USER_UNIT": "journal_user_unit",
		"SYSLOG_IDENTIFIER":  "journal_identifier",
		"PRIORITY":           "journal_priority",
		"_PID":               "journal_pid",
		"_TRANSPORT":         "journal_transport",
	} {
		if value, ok := journalScalar(fields[journalKey]); ok {
			if metadata == nil {
				metadata = make(map[string]any)
			}
			metadata[metadataKey] = value
		}
	}
	return metadata
}

func journalString(value any) (string, bool) {
	s, ok := value.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

func journalScalar(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil, false
		}
		return v, true
	case json.Number:
		return v, true
	case bool:
		return v, true
	default:
		return nil, false
	}
}

func journalTimestamp(value any) time.Time {
	s, ok := journalString(value)
	if !ok {
		return time.Now()
	}
	micros, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.Unix(0, micros*1000)
}

func (s *journalSource) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cmd := s.cmd
	done := s.done
	timeout := s.terminationTimeout
	s.mu.Unlock()
	if cmd == nil {
		return nil
	}
	requestTerminateProcess(cmd)
	go func() {
		if done == nil {
			return
		}
		if timeout == 0 {
			forceKillProcess(cmd)
			return
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			forceKillProcess(cmd)
		}
	}()
	return nil
}

func (s *journalSource) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func readLimitedString(r io.Reader, limit int64) string {
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, io.LimitReader(r, limit))
	return string(bytes.TrimSpace(buf.Bytes()))
}
