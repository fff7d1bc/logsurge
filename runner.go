package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

type Runner struct {
	Config Config
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (r Runner) Run() int {
	err := r.run()
	if err == nil {
		return 0
	}
	var exit exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	if r.Stderr != nil {
		fmt.Fprintf(r.Stderr, "logsurge: %v\n", err)
	}
	return 1
}

func (r Runner) run() error {
	// Hidden profiling hooks profile the receiver process. Tests run the
	// producer as a normal child so producer CPU/heap do not pollute receiver
	// profiles.
	stopProfiles, err := startDebugProfiles(r.Config)
	if err != nil {
		return err
	}
	defer stopProfiles()
	stdout := r.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := r.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := r.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stats := NewRuntimeStats()
	health, err := startHealthServer(r.Config.HealthListen, stats)
	if err != nil {
		return err
	}
	defer health.Close()
	if r.Config.ConfigMode {
		return r.runConfigMode(stdout, stderr, stats)
	}
	switch r.Config.Source {
	case SourceExec:
		return r.runExec(stdin, stdout, stats)
	case SourceStdin:
		return r.runReader(stdin, stdout, "stdin", nil, stats, "stdin")
	case SourceFile:
		reader := NewFollowFileReader(r.Config.FilePath, r.Config.FileStart, r.Config.FilePollInterval)
		stopSignals := r.closeOnSignals(reader)
		defer stopSignals()
		source := r.Config.SourceName
		if source == "" {
			source = r.Config.FilePath
		}
		return r.runReader(reader, stdout, source, func(error) { _ = reader.Close() }, stats, "file")
	case SourceListen:
		return r.runListen(stdout, stats)
	case "":
		return r.runExec(stdin, stdout, stats)
	default:
		return fmt.Errorf("unsupported input source %q", r.Config.Source)
	}
}

func (r Runner) runConfigMode(stdout io.Writer, stderr io.Writer, stats *RuntimeStats) error {
	formatter := r.formatter()
	sink, err := NewSinkWithDiagnostics(r.Config, stdout, stderr, formatter)
	if err != nil {
		return err
	}
	// Daemon mode has one output sink shared by many input pipelines. The lock
	// keeps formatted records from concurrent inputs from interleaving inside
	// non-thread-safe sinks such as directory and HTTP.
	sharedSink := &lockedSink{sink: sink}
	closers := make([]io.Closer, 0, len(r.Config.Inputs))
	var closersMu sync.Mutex
	closeAll := func() {
		// Source failures, sink failures, and setup errors can all request a
		// daemon shutdown. Keep the closer list synchronized while inputs are
		// still being started.
		closersMu.Lock()
		defer closersMu.Unlock()
		for _, closer := range closers {
			_ = closer.Close()
		}
	}
	var closeOnce sync.Once
	stopAll := func() { closeOnce.Do(closeAll) }
	stopSignals := r.closeOnSignals(closeFunc(stopAll))
	defer stopSignals()

	writerDone := make(chan error, len(r.Config.Inputs))
	for _, input := range r.Config.Inputs {
		// Each input owns its queue and framing limits. Do not collapse this
		// back to r.Config defaults; noisy files must not starve quieter ones.
		inputCfg := r.Config
		inputCfg.QueueRecords = input.QueueRecords
		inputCfg.QueueBytes = input.QueueBytes
		inputCfg.Overflow = input.Overflow
		inputCfg.MaxFragmentBytes = input.MaxFragmentBytes
		inputCfg.PartialFlushInterval = input.PartialFlushInterval
		inputCfg.ANSI = input.ANSI
		// The input runner keeps global sink/format settings, but gets its own
		// queue, framing, and ANSI settings so one noisy file cannot dominate
		// the behavior of quieter inputs.
		inputRunner := Runner{Config: inputCfg}
		q := NewQueue(input.QueueRecords, input.QueueBytes, input.Overflow)
		inputStats := stats.RegisterInput(string(input.Kind), input.Source, q)
		var done <-chan error
		var closer io.Closer
		var err error
		switch input.Kind {
		case InputKindJournal:
			done, closer, err = inputRunner.startJournalPipelineToSink(input, q, sharedSink, inputStats, stats, func(error) {
				stopAll()
			}, false)
		case InputKindTCP, InputKindUDP:
			done, closer, err = inputRunner.startNetworkPipelineToSink(input, q, sharedSink, inputStats, stats, func(error) {
				stopAll()
			}, false)
		default:
			reader := NewFollowFileReader(input.Path, input.FileStart, input.FilePollInterval)
			closer = reader
			done, _, err = inputRunner.startQueuePipelineToSink(reader, q, sharedSink, input.Source, inputStats, stats, func(error) {
				stopAll()
			}, false) // daemon-mode writers share one sink; the parent closes it.
		}
		if err != nil {
			stopAll()
			_ = sink.Close()
			return err
		}
		closersMu.Lock()
		closers = append(closers, closer)
		closersMu.Unlock()
		go func(done <-chan error) {
			writerDone <- <-done
		}(done)
	}

	var firstErr error
	for range r.Config.Inputs {
		if err := <-writerDone; err != nil && firstErr == nil {
			firstErr = err
			stopAll()
		}
	}
	if err := sink.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (r Runner) runExec(stdin io.Reader, stdout io.Writer, stats *RuntimeStats) error {
	if len(r.Config.Command) == 0 {
		return errors.New("no command specified")
	}
	cmd := exec.Command(r.Config.Command[0], r.Config.Command[1:]...)
	cmd.Stdin = stdin
	configureProcess(cmd)

	capture, err := NewCapture(cmd)
	if err != nil {
		return err
	}
	defer capture.Close()

	var cancelOnce sync.Once
	cancelChild := func() {
		cancelOnce.Do(func() { requestTerminateProcess(cmd) })
	}

	if err := cmd.Start(); err != nil {
		_ = capture.Close()
		return err
	}
	childDone := make(chan error, 1)
	// Start Wait before closing the parent writer so even rare setup failures
	// after Start still have exactly one waiter and can be cleaned up.
	go func() { childDone <- cmd.Wait() }()
	if err := capture.CloseParentWriter(); err != nil {
		cancelChild()
		_ = capture.Reader.Close()
		_ = r.waitForChildAfterCancel(cmd, childDone)
		return err
	}

	writerDone, q, err := r.startPipeline(capture.Reader, stdout, "combined", func(error) {
		cancelChild()
		_ = capture.Reader.Close()
	}, stats, "exec")
	if err != nil {
		cancelChild()
		_ = capture.Reader.Close()
		_ = r.waitForChildAfterCancel(cmd, childDone)
		return err
	}

	stopSignals := r.forwardSignals(cmd)
	defer stopSignals()

	select {
	case writerErr := <-writerDone:
		cancelChild()
		_ = capture.Reader.Close()
		childErr := r.waitForChildAfterCancel(cmd, childDone)
		if writerErr != nil {
			return writerErr
		}
		if childErr != nil {
			return exitError{code: exitCodeFromError(childErr)}
		}
		return nil
	case childErr := <-childDone:
		if err := r.waitForWriterAfterChild(capture, q, writerDone); err != nil {
			return err
		}
		if childErr != nil {
			return exitError{code: exitCodeFromError(childErr)}
		}
		return nil
	}
}

func (r Runner) runReader(reader io.Reader, stdout io.Writer, source string, onError func(error), stats *RuntimeStats, kind string) error {
	writerDone, _, err := r.startPipeline(reader, stdout, source, onError, stats, kind)
	if err != nil {
		return err
	}
	return <-writerDone
}

func (r Runner) runListen(stdout io.Writer, stats *RuntimeStats) error {
	formatter := r.formatter()
	stderr := r.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	sink, err := NewSinkWithDiagnostics(r.Config, stdout, stderr, formatter)
	if err != nil {
		return err
	}
	input := InputConfig{
		Kind:                 InputKind(r.Config.ListenNetwork),
		Listen:               r.Config.ListenAddress,
		MaxConnections:       r.Config.ListenMaxConnections,
		Source:               r.Config.ListenNetwork + "://" + r.Config.ListenAddress,
		QueueRecords:         r.Config.QueueRecords,
		QueueBytes:           r.Config.QueueBytes,
		Overflow:             r.Config.Overflow,
		MaxFragmentBytes:     r.Config.MaxFragmentBytes,
		PartialFlushInterval: r.Config.PartialFlushInterval,
		ANSI:                 r.Config.ANSI,
	}
	q := NewQueue(input.QueueRecords, input.QueueBytes, input.Overflow)
	inputStats := stats.RegisterInput(string(input.Kind), input.Source, q)
	done, closer, err := r.startNetworkPipelineToSink(input, q, sink, inputStats, stats, nil, true)
	if err != nil {
		_ = sink.Close()
		return err
	}
	stopSignals := r.closeOnSignals(closer)
	defer stopSignals()
	return <-done
}

func (r Runner) startPipeline(reader io.Reader, stdout io.Writer, source string, onError func(error), stats *RuntimeStats, kind string) (<-chan error, *Queue, error) {
	formatter := r.formatter()
	stderr := r.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	sink, err := NewSinkWithDiagnostics(r.Config, stdout, stderr, formatter)
	if err != nil {
		return nil, nil, err
	}
	return r.startPipelineToSink(reader, sink, source, kind, nil, stats, onError, true)
}

func (r Runner) startPipelineToSink(reader io.Reader, sink Sink, source string, kind string, inputStats *InputStats, runtimeStats *RuntimeStats, onError func(error), closeSink bool) (<-chan error, *Queue, error) {
	q := NewQueue(r.Config.QueueRecords, r.Config.QueueBytes, r.Config.Overflow)
	if inputStats == nil && runtimeStats != nil {
		inputStats = runtimeStats.RegisterInput(kind, source, q)
	}
	return r.startQueuePipelineToSink(reader, q, sink, source, inputStats, runtimeStats, onError, closeSink)
}

func (r Runner) startQueuePipelineToSink(reader io.Reader, q *Queue, sink Sink, source string, inputStats *InputStats, runtimeStats *RuntimeStats, onError func(error), closeSink bool) (<-chan error, *Queue, error) {
	metadata := staticMetadataSnapshot(r.Config.MetadataFields, r.Config.CustomMetadata)
	chunks := StartChunkReader(reader, 32*1024)
	framerDone := make(chan error, 1)
	go func() {
		framerDone <- RunFramer(chunks, q, FramerConfig{
			MaxFragmentBytes:     r.Config.MaxFragmentBytes,
			PartialFlushInterval: r.Config.PartialFlushInterval,
			Metadata:             metadata,
			Source:               source,
		})
	}()
	writerDone := r.startQueueWriterToSink(q, sink, inputStats, runtimeStats, onError, closeSink)
	return combineSourceAndWriter(framerDone, writerDone, inputStats), q, nil
}

func combineSourceAndWriter(sourceDone <-chan error, writerDone <-chan error, inputStats *InputStats) <-chan error {
	done := make(chan error, 1)
	go func() {
		writerErr := <-writerDone
		if writerErr != nil {
			// Sink failures may close the source reader to unblock the framer.
			// Prefer the sink error and do not report the induced read error as
			// a source failure.
			done <- writerErr
			return
		}
		sourceErr := <-sourceDone
		if errors.Is(sourceErr, os.ErrClosed) {
			sourceErr = nil
		}
		if sourceErr != nil && inputStats != nil {
			inputStats.SourceErrors.Add(1)
		}
		done <- sourceErr
	}()
	return done
}

func (r Runner) startQueueWriterToSink(q *Queue, sink Sink, inputStats *InputStats, runtimeStats *RuntimeStats, onError func(error), closeSink bool) <-chan error {
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- RunWriterWithStats(q, sinkForWriter(sink, closeSink), r.Config.FlushInterval, r.Config.ANSI, func(err error) {
			if runtimeStats != nil {
				runtimeStats.IncSinkErrors()
			}
			if onError != nil {
				onError(err)
			}
		}, inputStats)
	}()
	return writerDone
}

func (r Runner) startJournalPipelineToSink(input InputConfig, q *Queue, sink Sink, inputStats *InputStats, runtimeStats *RuntimeStats, onError func(error), closeSink bool) (<-chan error, io.Closer, error) {
	metadata := staticMetadataSnapshot(r.Config.MetadataFields, r.Config.CustomMetadata)
	source := newJournalSource(input, metadata, r.Config.MaxFragmentBytes, r.Config.TerminationTimeout)
	writerDone := r.startQueueWriterToSink(q, sink, inputStats, runtimeStats, func(err error) {
		_ = source.Close()
		if onError != nil {
			onError(err)
		}
	}, closeSink)
	sourceDone, err := source.Start(q)
	if err != nil {
		q.Close()
		return nil, source, err
	}
	done := make(chan error, 1)
	go func() {
		sourceErr := <-sourceDone
		if sourceErr != nil && inputStats != nil {
			inputStats.SourceErrors.Add(1)
		}
		writerErr := <-writerDone
		if sourceErr != nil {
			done <- sourceErr
			return
		}
		done <- writerErr
	}()
	return done, source, nil
}

func (r Runner) startNetworkPipelineToSink(input InputConfig, q *Queue, sink Sink, inputStats *InputStats, runtimeStats *RuntimeStats, onError func(error), closeSink bool) (<-chan error, io.Closer, error) {
	metadata := staticMetadataSnapshot(r.Config.MetadataFields, r.Config.CustomMetadata)
	source := newNetworkSource(input, metadata, inputStats)
	writerDone := r.startQueueWriterToSink(q, sink, inputStats, runtimeStats, func(err error) {
		_ = source.Close()
		if onError != nil {
			onError(err)
		}
	}, closeSink)
	sourceDone, err := source.Start(q)
	if err != nil {
		q.Close()
		return nil, source, err
	}
	done := make(chan error, 1)
	go func() {
		sourceErr := <-sourceDone
		if sourceErr != nil && inputStats != nil {
			inputStats.SourceErrors.Add(1)
		}
		writerErr := <-writerDone
		if sourceErr != nil {
			done <- sourceErr
			return
		}
		done <- writerErr
	}()
	return done, source, nil
}

func (r Runner) formatter() Formatter {
	var formatter Formatter
	switch r.Config.Format {
	case FormatJSON:
		formatter = JSONFormatter{MetadataFields: r.Config.MetadataFields}
	default:
		formatter = PlainFormatter{IncludeSource: r.Config.ConfigMode}
	}
	return formatter
}

func startDebugProfiles(cfg Config) (func(), error) {
	var cpuFile *os.File
	if cfg.DebugCPUProfile != "" {
		f, err := os.Create(cfg.DebugCPUProfile)
		if err != nil {
			return nil, err
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			_ = f.Close()
			return nil, err
		}
		cpuFile = f
	}
	return func() {
		if cpuFile != nil {
			pprof.StopCPUProfile()
			_ = cpuFile.Close()
		}
		if cfg.DebugMemProfile != "" {
			// Force a collection so the heap profile is useful for retained
			// memory, not just transient allocation noise from the just-finished
			// flood.
			runtime.GC()
			f, err := os.Create(cfg.DebugMemProfile)
			if err != nil {
				return
			}
			_ = pprof.WriteHeapProfile(f)
			_ = f.Close()
		}
	}, nil
}

func (r Runner) waitForChildAfterCancel(cmd *exec.Cmd, childDone <-chan error) error {
	if r.Config.TerminationTimeout == 0 {
		forceKillProcess(cmd)
		return <-childDone
	}
	timer := time.NewTimer(r.Config.TerminationTimeout)
	defer timer.Stop()
	select {
	case err := <-childDone:
		return err
	case <-timer.C:
		forceKillProcess(cmd)
		return <-childDone
	}
}

func (r Runner) waitForWriterAfterChild(capture *Capture, q *Queue, writerDone <-chan error) error {
	if r.Config.PostExitDrainTimeout == 0 {
		err := <-writerDone
		return err
	}
	timer := time.NewTimer(r.Config.PostExitDrainTimeout)
	defer timer.Stop()
	select {
	case err := <-writerDone:
		return err
	case <-timer.C:
		// Grandchildren can inherit stdout/stderr and keep the pipe open after
		// the direct child exits. Close our read side after a grace period so
		// daemonizing commands cannot hang logsurge forever.
		// This diagnostic is best-effort; a full queue may drop it, but the
		// capture close below is the behavior that matters.
		q.PushInternal(Record{
			Time:          time.Now(),
			End:           RecordEndInternal,
			InternalEvent: "post_exit_drain_timeout",
			Message:       "post-exit drain timeout reached; closing inherited stdout/stderr",
			Reason:        "stdout_still_open",
		})
		_ = capture.Reader.Close()
		err := <-writerDone
		return err
	}
}

func (r Runner) closeOnSignals(closer io.Closer) func() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, handledSignals()...)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				_ = closer.Close()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

func (r Runner) forwardSignals(cmd *exec.Cmd) func() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, handledSignals()...)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				forwardSignal(cmd, sig)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("command exited with status %d", e.code)
}
