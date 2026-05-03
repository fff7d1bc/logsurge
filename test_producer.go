package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
)

func runTestProducer(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("__test-producer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	records := fs.Int("records", 1000, "records to write")
	lineBytes := fs.Int("line-bytes", 120, "bytes per line before newline")
	charset := fs.String("charset", "ascii", "line payload charset: ascii, utf8, or ansi")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *records < 0 || *lineBytes < 1 {
		fmt.Fprintln(stderr, "__test-producer: records must be non-negative and line-bytes must be positive")
		return 2
	}
	if *charset != "ascii" && *charset != "utf8" && *charset != "ansi" {
		fmt.Fprintln(stderr, "__test-producer: charset must be ascii, utf8, or ansi")
		return 2
	}
	if err := writeTestProducer(stdout, *records, *lineBytes, *charset); err != nil {
		if !errors.Is(err, io.ErrClosedPipe) {
			fmt.Fprintf(stderr, "__test-producer: %v\n", err)
		}
		return 1
	}
	return 0
}

func writeTestProducer(out io.Writer, records int, lineBytes int, charset string) error {
	// Keep the producer simple and faster than the receiver. Flood tests should
	// measure receiver behavior under pressure, not producer formatting cost.
	w := bufio.NewWriterSize(out, 256*1024)
	line := make([]byte, lineBytes)
	for i := 0; i < records; i++ {
		fillTestProducerLine(line, i, charset)
		line[len(line)-1] = '\n'
		if _, err := w.Write(line); err != nil {
			return err
		}
	}
	return w.Flush()
}

func fillTestProducerLine(line []byte, record int, charset string) {
	for j := range line {
		line[j] = 'x'
	}
	prefix := strconv.AppendInt(nil, int64(record), 10)
	bodyLen := len(line) - 1
	prefixLen := copy(line[:bodyLen], prefix)
	switch charset {
	case "utf8":
		fillPattern(line[prefixLen:bodyLen], []byte("/tmp/指定材料.webp "))
	case "ansi":
		fillPattern(line[prefixLen:bodyLen], []byte("\x1b[31mred\x1b[0m "))
	}
}

func fillPattern(dst []byte, pattern []byte) {
	if len(pattern) == 0 {
		return
	}
	for len(dst) > 0 {
		n := copy(dst, pattern)
		dst = dst[n:]
	}
}
