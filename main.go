package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__test-producer" {
		os.Exit(runTestProducer(os.Args[2:], os.Stdout, os.Stderr))
	}
	cfg, err := ParseConfig(os.Args[1:])
	if err != nil {
		if errors.Is(err, ErrHelp) {
			fmt.Fprint(os.Stdout, Usage())
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "logsurge: %v\n", err)
		fmt.Fprintln(os.Stderr, "try 'logsurge --help' for usage")
		os.Exit(2)
	}
	if cfg.Version {
		fmt.Fprintf(os.Stdout, "logsurge %s\n", Version)
		os.Exit(0)
	}
	runner := Runner{
		Config: cfg,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(runner.Run())
}
