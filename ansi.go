package main

import "bytes"

type ansiMode string

const (
	ANSIStrip ansiMode = "strip"
	ANSIKeep  ansiMode = "keep"
)

type ansiStripperState uint8

const (
	ansiStateNormal ansiStripperState = iota
	ansiStateEsc
	ansiStateCSI
	ansiStateOSC
	ansiStateOSCEsc
)

type ansiStripper struct {
	state ansiStripperState
	out   []byte
}

// ansiStripper is intentionally stateful. The framer can split a terminal
// escape across records at timeout or max-fragment boundaries, so stripping
// each record independently would leak escape fragments into output.
func (s *ansiStripper) Strip(p []byte) []byte {
	// Most log lines have no terminal controls. Return the queue-owned bytes as
	// is so default stripping does not copy the common path.
	if s.state == ansiStateNormal && bytes.IndexByte(p, 0x1b) < 0 {
		return p
	}
	// When stripping is needed, the returned slice points at reusable scratch.
	// Sinks must format/write synchronously or copy before retaining it.
	s.out = s.out[:0]
	for _, c := range p {
		switch s.state {
		case ansiStateNormal:
			switch c {
			case 0x1b:
				s.state = ansiStateEsc
			default:
				s.out = append(s.out, c)
			}
		case ansiStateEsc:
			switch c {
			case '[':
				s.state = ansiStateCSI
			case ']':
				s.state = ansiStateOSC
			case 0x1b:
				s.state = ansiStateEsc
			default:
				// Drop one-byte ESC controls and unknown ESC-prefixed controls.
				s.state = ansiStateNormal
			}
		case ansiStateCSI:
			if c >= 0x40 && c <= 0x7e {
				s.state = ansiStateNormal
			}
		case ansiStateOSC:
			// OSC sequences end either with BEL or with the two-byte ST marker
			// ESC \. Keep state across records because partial flushing can split
			// terminal sequences in the middle.
			switch c {
			case 0x07:
				s.state = ansiStateNormal
			case 0x1b:
				s.state = ansiStateOSCEsc
			}
		case ansiStateOSCEsc:
			if c == '\\' {
				s.state = ansiStateNormal
			} else if c != 0x1b {
				s.state = ansiStateOSC
			}
		}
	}
	// An incomplete trailing control sequence remains in state and is not
	// emitted. Leaking half an escape is worse for ingestion than dropping it.
	return s.out
}
