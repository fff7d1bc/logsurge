package main

import "sync"

type DropStats struct {
	Records uint64
	Bytes   uint64
	Policy  OverflowPolicy
}

type Queue struct {
	mu         sync.Mutex
	notFull    *sync.Cond
	wake       chan struct{}
	closed     bool
	records    []Record
	head       int
	queued     int
	queuedByte int
	maxRecords int
	maxBytes   int
	nextSeq    uint64
	overflow   OverflowPolicy
	drops      DropStats
	stats      *InputStats
}

func NewQueue(maxRecords, maxBytes int, overflow OverflowPolicy) *Queue {
	q := &Queue{
		// This is a slice-backed ring instead of a channel so both record count
		// and queued line bytes are bounded. A channel cannot enforce the byte
		// limit that protects small machines.
		wake:       make(chan struct{}, 1),
		records:    make([]Record, maxRecords),
		maxRecords: maxRecords,
		maxBytes:   maxBytes,
		overflow:   overflow,
	}
	q.notFull = sync.NewCond(&q.mu)
	return q
}

type QueueSnapshot struct {
	Records    int
	Bytes      int
	MaxRecords int
	MaxBytes   int
}

func (q *Queue) SetStats(stats *InputStats) {
	// Metrics are deliberately attached outside NewQueue so tests and ad-hoc
	// runs can use queues without constructing the runtime metrics registry.
	q.mu.Lock()
	q.stats = stats
	q.mu.Unlock()
}

func (q *Queue) Snapshot() QueueSnapshot {
	// Snapshot is point-in-time observability for health/metrics endpoints.
	// Queue correctness still comes from the mutex-protected ring state.
	q.mu.Lock()
	defer q.mu.Unlock()
	return QueueSnapshot{
		Records:    q.queued,
		Bytes:      q.queuedByte,
		MaxRecords: q.maxRecords,
		MaxBytes:   q.maxBytes,
	}
}

func (q *Queue) Push(meta RecordMeta, line []byte) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.overflow == OverflowBlock {
		// Reliability mode deliberately backpressures the framer and eventually
		// the child pipe. Lossy modes below must never wait for capacity.
		for !q.closed && !q.hasCapacityLocked(len(line)) {
			q.notFull.Wait()
		}
		if q.closed {
			return false
		}
		return q.pushLocked(meta, line)
	}
	if q.closed {
		return false
	}
	if !q.hasCapacityLocked(len(line)) {
		if q.overflow == OverflowDropOldest {
			return q.pushDropOldestLocked(meta, line)
		}
		q.drops.Records++
		q.drops.Bytes += uint64(len(line))
		q.drops.Policy = OverflowDropNewest
		q.addDropStatsLocked(1, len(line))
		return false
	}
	return q.pushLocked(meta, line)
}

func (q *Queue) PushInternal(rec Record) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	size := len(rec.Line)
	// Internal diagnostics are best-effort. If the queue is already full, do not
	// evict user log records just to report a timeout/drop message.
	if q.closed || !q.hasCapacityLocked(size) {
		return false
	}
	cp := make([]byte, len(rec.Line))
	copy(cp, rec.Line)
	rec.Line = cp
	rec.Seq = q.nextSeq
	q.nextSeq++
	idx := (q.head + q.queued) % len(q.records)
	q.records[idx] = rec
	q.queued++
	q.queuedByte += size
	q.signalLocked()
	return true
}

func (q *Queue) pushLocked(meta RecordMeta, line []byte) bool {
	// Copy only after capacity is known. This keeps rejected records cheap and
	// makes accepted records immutable from the framer's point of view.
	cp := make([]byte, len(line))
	copy(cp, line)
	rec := Record{
		Time:      meta.Time,
		Line:      cp,
		Seq:       q.nextSeq,
		End:       meta.End,
		Source:    meta.Source,
		Continued: meta.Continued,
		Metadata:  meta.Metadata,
	}
	q.nextSeq++
	idx := (q.head + q.queued) % len(q.records)
	q.records[idx] = rec
	q.queued++
	q.queuedByte += len(cp)
	if q.stats != nil {
		q.stats.AcceptedRecords.Add(1)
		q.stats.AcceptedBytes.Add(uint64(len(cp)))
	}
	q.signalLocked()
	return true
}

func (q *Queue) pushDropOldestLocked(meta RecordMeta, line []byte) bool {
	size := len(line)
	if size > q.maxBytes {
		// No amount of eviction can make an oversized record fit under the hard
		// byte cap, so drop the incoming record and keep the queue bounded.
		q.drops.Records++
		q.drops.Bytes += uint64(size)
		q.drops.Policy = OverflowDropOldest
		q.addDropStatsLocked(1, size)
		return false
	}
	for !q.hasCapacityLocked(size) && q.queued > 0 {
		rec := q.records[q.head]
		// Clear the slot so the old line byte slice can be collected. This is
		// important during long floods on memory-constrained hosts.
		q.records[q.head] = Record{}
		q.head = (q.head + 1) % len(q.records)
		q.queued--
		q.queuedByte -= len(rec.Line)
		q.drops.Records++
		q.drops.Bytes += uint64(len(rec.Line))
		q.drops.Policy = OverflowDropOldest
		q.addDropStatsLocked(1, len(rec.Line))
	}
	if !q.hasCapacityLocked(size) {
		q.drops.Records++
		q.drops.Bytes += uint64(size)
		q.drops.Policy = OverflowDropOldest
		q.addDropStatsLocked(1, size)
		return false
	}
	return q.pushLocked(meta, line)
}

func (q *Queue) addDropStatsLocked(records uint64, bytes int) {
	if q.stats != nil {
		// These counters are cumulative and best-effort; q.drops below is the
		// per-interval state consumed by writer diagnostics.
		q.stats.DroppedRecords.Add(records)
		q.stats.DroppedBytes.Add(uint64(bytes))
	}
}

func (q *Queue) hasCapacityLocked(size int) bool {
	return q.queued < q.maxRecords && q.queuedByte+size <= q.maxBytes
}

func (q *Queue) Pop() (Record, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued == 0 {
		return Record{}, false
	}
	rec := q.records[q.head]
	q.records[q.head] = Record{}
	q.head = (q.head + 1) % len(q.records)
	q.queued--
	q.queuedByte -= len(rec.Line)
	q.notFull.Signal()
	return rec, true
}

func (q *Queue) Wake() <-chan struct{} {
	return q.wake
}

func (q *Queue) Close() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.wake)
		q.notFull.Broadcast()
	}
	q.mu.Unlock()
}

func (q *Queue) TakeDrops() DropStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	d := q.drops
	q.drops = DropStats{}
	return d
}

func (q *Queue) signalLocked() {
	// Coalesce wakeups. The writer drains until empty, so one pending wake token
	// is enough no matter how many records arrived.
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
