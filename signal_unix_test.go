//go:build unix

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestForwardSignalToProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "trap 'echo gotterm; exit 42' TERM; sleep 10")
	configureProcess(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	time.Sleep(50 * time.Millisecond)
	forwardSignal(cmd, syscall.SIGTERM)
	select {
	case err := <-done:
		if exitCodeFromError(err) != 42 {
			t.Fatalf("exit = %d err=%v output=%q", exitCodeFromError(err), err, out.String())
		}
	case <-time.After(2 * time.Second):
		forceKillProcess(cmd)
		t.Fatal("process group did not receive signal")
	}
	if !strings.Contains(out.String(), "gotterm") {
		t.Fatalf("output = %q", out.String())
	}
}
