package main

import "testing"

func TestANSIStripperRemovesCSI(t *testing.T) {
	var s ansiStripper
	got := s.Strip([]byte("a\x1b[31mb\x1b[0mc"))
	if string(got) != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperRemovesSplitCSI(t *testing.T) {
	var s ansiStripper
	if got := s.Strip([]byte("a\x1b[3")); string(got) != "a" {
		t.Fatalf("first got %q", got)
	}
	if got := s.Strip([]byte("1mb")); string(got) != "b" {
		t.Fatalf("second got %q", got)
	}
}

func TestANSIStripperRemovesOSC(t *testing.T) {
	var s ansiStripper
	got := s.Strip([]byte("a\x1b]8;;https://example.test\x07label\x1b]8;;\x07b"))
	if string(got) != "alabelb" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperDropsIncompleteSequence(t *testing.T) {
	var s ansiStripper
	got := s.Strip([]byte("a\x1b[31"))
	if string(got) != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestANSIStripperKeepsUTF8BytesThatOverlapC1Controls(t *testing.T) {
	tests := []string{
		"/tmp/指定材料.webp",
		"/tmp/开启擦拭塔.webp",
		"/tmp/多耗材打印功能.webp",
		"/tmp/供电1.webp",
		"/tmp/磁环2.webp",
		"plain-ascii",
	}
	for _, line := range tests {
		t.Run(line, func(t *testing.T) {
			var s ansiStripper
			got := s.Strip([]byte(line))
			if string(got) != line {
				t.Fatalf("got %q", got)
			}
			next := s.Strip([]byte("/tmp/next"))
			if string(next) != "/tmp/next" {
				t.Fatalf("next got %q", next)
			}
		})
	}
}
