package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTestProducerCharsets(t *testing.T) {
	for _, charset := range []string{"ascii", "utf8", "ansi"} {
		t.Run(charset, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := runTestProducer([]string{"--records", "2", "--line-bytes", "80", "--charset", charset}, &out, &stderr)
			if code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if len(lines) != 2 {
				t.Fatalf("lines=%q", out.String())
			}
			if len(lines[0])+1 != 80 {
				t.Fatalf("line len = %d", len(lines[0])+1)
			}
			switch charset {
			case "utf8":
				if !strings.Contains(lines[0], "指定材料") {
					t.Fatalf("utf8 line = %q", lines[0])
				}
			case "ansi":
				if !strings.Contains(lines[0], "\x1b[31mred\x1b[0m") {
					t.Fatalf("ansi line = %q", lines[0])
				}
			}
		})
	}
}

func TestTestProducerRejectsBadCharset(t *testing.T) {
	var out, stderr bytes.Buffer
	code := runTestProducer([]string{"--charset", "bad"}, &out, &stderr)
	if code == 0 {
		t.Fatal("expected nonzero")
	}
	if !strings.Contains(stderr.String(), "charset must be ascii, utf8, or ansi") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
