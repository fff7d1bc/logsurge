package main

import (
	"bytes"
	"io"
	"testing"
	"time"
)

var benchRecordTime = time.Unix(1_700_000_000, 123456789)

func BenchmarkFramerLongLines(b *testing.B) {
	data := benchmarkLines(4096, 120, "ascii")
	b.ReportAllocs()
	b.SetBytes(120)
	b.StopTimer()
	for processed := 0; processed < b.N; {
		batch := min(4096, b.N-processed)
		ch := make(chan chunkResult, 1)
		ch <- chunkResult{data: data[:batch*120]}
		close(ch)
		q := NewQueue(batch, batch*120, OverflowBlock)
		b.StartTimer()
		RunFramer(ch, q, FramerConfig{MaxFragmentBytes: 64 * 1024})
		b.StopTimer()
		for {
			if _, ok := q.Pop(); !ok {
				break
			}
		}
		processed += batch
	}
}

func BenchmarkQueuePushPop(b *testing.B) {
	line := bytes.Repeat([]byte("x"), 120)
	meta := RecordMeta{Time: benchRecordTime, End: RecordEndNewline}
	for _, tc := range []struct {
		name     string
		overflow OverflowPolicy
	}{
		{name: "block", overflow: OverflowBlock},
		{name: "drop_oldest", overflow: OverflowDropOldest},
	} {
		b.Run(tc.name, func(b *testing.B) {
			q := NewQueue(8192, 8192*len(line), tc.overflow)
			b.ReportAllocs()
			b.SetBytes(int64(len(line)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !q.Push(meta, line) {
					b.Fatal("push dropped")
				}
				if _, ok := q.Pop(); !ok {
					b.Fatal("pop failed")
				}
			}
		})
	}
}

func BenchmarkWriterPlainDiscard(b *testing.B) {
	benchmarkWriter(b, PlainFormatter{}, ANSIKeep, "ascii", nil)
}

func BenchmarkWriterJSONDiscard(b *testing.B) {
	benchmarkWriter(b, JSONFormatter{}, ANSIKeep, "ascii", nil)
}

func BenchmarkWriterJSONMetadataDiscard(b *testing.B) {
	metadata := map[string]any{"hostname": "builder", "terraform_run": "retry"}
	formatter := JSONFormatter{MetadataFields: []MetadataField{MetadataSource, MetadataLineEnd, MetadataContinued}}
	benchmarkWriter(b, formatter, ANSIKeep, "ascii", metadata)
}

func BenchmarkWriterANSIStripUTF8(b *testing.B) {
	benchmarkWriter(b, PlainFormatter{}, ANSIStrip, "utf8", nil)
}

func BenchmarkWriterANSIStripEscapes(b *testing.B) {
	benchmarkWriter(b, PlainFormatter{}, ANSIStrip, "ansi", nil)
}

func BenchmarkHTTPJSONLineEncoder(b *testing.B) {
	benchmarkHTTPEncoder(b, JSONLineHTTPEncoder{}, nil)
}

func BenchmarkHTTPJSONLineEncoderMetadata(b *testing.B) {
	metadata := map[string]any{"hostname": "builder", "terraform_run": "retry"}
	enc := JSONLineHTTPEncoder{MetadataFields: []MetadataField{MetadataSource, MetadataLineEnd, MetadataContinued}}
	benchmarkHTTPEncoder(b, enc, metadata)
}

func BenchmarkHTTPGELFEncoder(b *testing.B) {
	benchmarkHTTPEncoder(b, GELFHTTPEncoder{Host: "bench-host"}, nil)
}

func BenchmarkHTTPGELFEncoderMetadata(b *testing.B) {
	metadata := map[string]any{"hostname": "builder", "terraform_run": "retry"}
	enc := GELFHTTPEncoder{Host: "bench-host", MetadataFields: []MetadataField{MetadataSource, MetadataLineEnd, MetadataContinued}}
	benchmarkHTTPEncoder(b, enc, metadata)
}

func benchmarkWriter(b *testing.B, formatter Formatter, ansi ansiMode, charset string, metadata map[string]any) {
	const batch = 4096
	lines := benchmarkLineSet(batch, 120, charset)
	b.ReportAllocs()
	b.SetBytes(120)
	b.StopTimer()
	for processed := 0; processed < b.N; {
		n := min(batch, b.N-processed)
		q := NewQueue(n+1, (n+1)*256, OverflowBlock)
		for i := 0; i < n; i++ {
			q.Push(RecordMeta{
				Time:     benchRecordTime,
				End:      RecordEndNewline,
				Source:   "bench",
				Metadata: metadata,
			}, lines[i])
		}
		q.Close()
		sink := NewStdoutSink(io.Discard, formatter)
		b.StartTimer()
		if err := RunWriter(q, sink, 0, ansi, nil); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		processed += n
	}
}

func benchmarkHTTPEncoder(b *testing.B, enc HTTPRecordEncoder, metadata map[string]any) {
	line := bytes.Repeat([]byte("x"), 120)
	record := Record{
		Time:      benchRecordTime,
		Line:      line,
		End:       RecordEndNewline,
		Source:    "bench",
		Continued: true,
		Metadata:  metadata,
	}
	var out bytes.Buffer
	b.ReportAllocs()
	b.SetBytes(int64(len(line)))
	for i := 0; i < b.N; i++ {
		out.Reset()
		if err := enc.Encode(record, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkLineSet(records int, lineBytes int, charset string) [][]byte {
	out := make([][]byte, records)
	for i := 0; i < records; i++ {
		line := make([]byte, lineBytes)
		fillTestProducerLine(line, i, charset)
		out[i] = line[:lineBytes-1]
	}
	return out
}

func benchmarkLines(records int, lineBytes int, charset string) []byte {
	var out bytes.Buffer
	_ = writeTestProducer(&out, records, lineBytes, charset)
	return out.Bytes()
}
