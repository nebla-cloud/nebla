package main

import (
	"cmp"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// healthURL is the loopback healthz URL for a PANEL_LISTEN value.
func healthURL(listen string) (string, error) {
	_, port, err := net.SplitHostPort(cmp.Or(listen, ":8080"))
	if err != nil {
		return "", err
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + "/v1/healthz", nil
}

func checkHealth(url string) error {
	hc := &http.Client{Timeout: 5 * time.Second}
	res, err := hc.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", res.StatusCode)
	}
	return nil
}

// healthcheck backs `nebla-server healthcheck` for container healthchecks; it returns the exit code.
func healthcheck() int {
	url, err := healthURL(os.Getenv("PANEL_LISTEN"))
	if err == nil {
		err = checkHealth(url)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	return 0
}
