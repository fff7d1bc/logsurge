package main

import (
	"bytes"
	"io"
	"net"
	"sync"
	"time"
)

type networkSource struct {
	kind             InputKind
	address          string
	source           string
	maxConnections   int
	maxFragmentBytes int
	partialFlush     time.Duration
	metadata         map[string]any
	stats            *InputStats

	mu       sync.Mutex
	closed   bool
	listener net.Listener
	packet   net.PacketConn
	conns    map[net.Conn]struct{}
	active   int
}

func newNetworkSource(input InputConfig, metadata map[string]any, stats *InputStats) *networkSource {
	return &networkSource{
		kind:             input.Kind,
		address:          input.Listen,
		source:           input.Source,
		maxConnections:   input.MaxConnections,
		maxFragmentBytes: input.MaxFragmentBytes,
		partialFlush:     input.PartialFlushInterval,
		metadata:         metadata,
		stats:            stats,
		conns:            make(map[net.Conn]struct{}),
	}
}

func (s *networkSource) Start(q *Queue) (<-chan error, error) {
	switch s.kind {
	case InputKindTCP:
		return s.startTCP(q)
	case InputKindUDP:
		return s.startUDP(q)
	default:
		return nil, io.ErrUnexpectedEOF
	}
}

func (s *networkSource) startTCP(q *Queue) (<-chan error, error) {
	ln, err := net.Listen("tcp", s.address)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	done := make(chan error, 1)
	var wg sync.WaitGroup
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if s.isClosed() {
					break
				}
				done <- err
				q.Close()
				return
			}
			if !s.addConn(conn) {
				_ = conn.Close()
				continue
			}
			if s.stats != nil {
				s.stats.TCPAccepted.Add(1)
				s.stats.TCPActive.Add(1)
			}
			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer s.removeConn(conn)
				// Every TCP connection has independent line-framing state, but
				// all accepted connections share this input's bounded queue.
				chunks := StartChunkReader(conn, 32*1024)
				RunFramerNoClose(chunks, q, FramerConfig{
					MaxFragmentBytes:     s.maxFragmentBytes,
					PartialFlushInterval: s.partialFlush,
					Metadata:             s.metadata,
					Source:               s.source,
				})
			}(conn)
		}
		wg.Wait()
		q.Close()
		done <- nil
	}()
	return done, nil
}

func (s *networkSource) startUDP(q *Queue) (<-chan error, error) {
	conn, err := net.ListenPacket("udp", s.address)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.packet = conn
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				if s.isClosed() {
					break
				}
				done <- err
				q.Close()
				return
			}
			if s.stats != nil {
				s.stats.UDPDatagrams.Add(1)
			}
			// UDP has no stream boundary to preserve after a datagram arrives.
			// Treat each datagram as one logical line and split only for the
			// same max-fragment limit used by stream inputs.
			line := bytes.TrimRight(buf[:n], "\r\n")
			s.pushDatagram(q, line)
		}
		q.Close()
		done <- nil
	}()
	return done, nil
}

func (s *networkSource) pushDatagram(q *Queue, line []byte) {
	if s.maxFragmentBytes <= 0 || len(line) <= s.maxFragmentBytes {
		q.Push(RecordMeta{
			Time:     time.Now(),
			End:      RecordEndNewline,
			Source:   s.source,
			Metadata: s.metadata,
		}, line)
		return
	}
	remaining := line
	continued := false
	for len(remaining) > s.maxFragmentBytes {
		q.Push(RecordMeta{
			Time:      time.Now(),
			End:       RecordEndMaxBytes,
			Source:    s.source,
			Continued: continued,
			Metadata:  s.metadata,
		}, remaining[:s.maxFragmentBytes])
		remaining = remaining[s.maxFragmentBytes:]
		continued = true
	}
	q.Push(RecordMeta{
		Time:      time.Now(),
		End:       RecordEndNewline,
		Source:    s.source,
		Continued: continued,
		Metadata:  s.metadata,
	}, remaining)
}

func (s *networkSource) addConn(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active >= s.maxConnections {
		return false
	}
	s.conns[conn] = struct{}{}
	s.active++
	return true
}

func (s *networkSource) removeConn(conn net.Conn) {
	s.mu.Lock()
	if _, ok := s.conns[conn]; ok {
		delete(s.conns, conn)
		s.active--
	}
	s.mu.Unlock()
	_ = conn.Close()
	if s.stats != nil {
		s.stats.TCPClosed.Add(1)
		s.stats.TCPActive.Add(-1)
	}
}

func (s *networkSource) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.listener
	packet := s.packet
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if packet != nil {
		_ = packet.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	return nil
}

func (s *networkSource) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
