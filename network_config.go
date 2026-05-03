package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
)

func parseListenSpec(spec string) (string, string, error) {
	u, err := url.Parse(spec)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "tcp" && u.Scheme != "udp" {
		return "", "", fmt.Errorf("unsupported network %q", u.Scheme)
	}
	if u.Host == "" || u.Path != "" {
		return "", "", errors.New("expected tcp://HOST:PORT or udp://HOST:PORT")
	}
	if err := validateLoopbackAddress(u.Host); err != nil {
		return "", "", err
	}
	return u.Scheme, u.Host, nil
}

func validateLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if port == "" {
		return errors.New("port is required")
	}
	if host == "localhost" {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q did not resolve", host)
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return fmt.Errorf("host %q is not loopback", host)
		}
	}
	return nil
}
