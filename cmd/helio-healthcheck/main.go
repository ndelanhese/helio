package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	healthURL            = "http://127.0.0.1:8080/health/ready"
	maxResponseBodyBytes = 4096
	probeTimeout         = 2500 * time.Millisecond
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	if err := run(ctx, healthURL); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, url string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request readiness: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read readiness response: %w", err)
	}
	if len(body) > maxResponseBodyBytes {
		return errors.New("readiness response exceeds size limit")
	}

	var payload struct {
		Status string `json:"status"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("decode readiness response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode readiness response: expected one JSON object")
	}
	if payload.Status != "ready" {
		return fmt.Errorf("readiness status is %q", payload.Status)
	}

	return nil
}
