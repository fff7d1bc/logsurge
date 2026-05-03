//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	// Put the child in its own process group. Later negative-PID signals target
	// the whole tree that kept stdout/stderr open, not just the direct child.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func requestTerminateProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		// Negative PID means process group on Unix.
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
}

func forceKillProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return
	}
	_ = cmd.Process.Kill()
}

func forwardSignal(cmd *exec.Cmd, sig os.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if s, ok := sig.(syscall.Signal); ok {
		_ = syscall.Kill(-cmd.Process.Pid, s)
	}
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*exec.ExitError); ok {
		if status, ok := e.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal())
			}
		}
		return e.ExitCode()
	}
	return 1
}
