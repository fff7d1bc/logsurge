package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

type healthServer struct {
	server *http.Server
	ln     net.Listener
}

func startHealthServer(address string, stats *RuntimeStats) (*healthServer, error) {
	if address == "" {
		return nil, nil
	}
	if err := validateLoopbackAddress(address); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		if stats != nil {
			stats.WritePrometheus(w)
		}
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	hs := &healthServer{server: server, ln: ln}
	go func() {
		_ = server.Serve(ln)
	}()
	return hs, nil
}

func (s *healthServer) Close() error {
	if s == nil || s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}
