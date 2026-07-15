package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/sofar"
)

func TestRunRequiresExplicitHardwareOptInWithoutReading(t *testing.T) {
	called := false
	err := run(&strings.Builder{}, func(string) string { return "" }, func(context.Context, sofar.HardwareConfig) (domain.TelemetrySnapshot, error) {
		called = true
		return domain.TelemetrySnapshot{}, nil
	})
	if err == nil || called {
		t.Fatalf("run error = %v, reader called = %t", err, called)
	}
}

func TestRunPrintsOneTelemetryJSONSnapshot(t *testing.T) {
	env := map[string]string{
		"HELIO_HARDWARE_TEST": "1",
		"HELIO_LOGGER_IP":     "10.0.0.5",
		"HELIO_LOGGER_SERIAL": "123456789",
		"HELIO_MODBUS_SLAVE":  "1",
	}
	want := domain.TelemetrySnapshot{
		ObservedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		Status:     "normal",
	}
	var output strings.Builder
	err := run(&output, func(key string) string { return env[key] }, func(context.Context, sofar.HardwareConfig) (domain.TelemetrySnapshot, error) {
		return want, nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("output must contain one line: %q", output.String())
	}
	for _, secret := range []string{"10.0.0.5", "123456789", "serial", "address"} {
		if strings.Contains(strings.ToLower(output.String()), strings.ToLower(secret)) {
			t.Fatalf("output exposes %q: %s", secret, output.String())
		}
	}
}

func TestRunRedactsTransportFailure(t *testing.T) {
	env := map[string]string{
		"HELIO_HARDWARE_TEST": "1",
		"HELIO_LOGGER_IP":     "10.0.0.5",
		"HELIO_LOGGER_SERIAL": "123456789",
		"HELIO_MODBUS_SLAVE":  "1",
	}
	err := run(&strings.Builder{}, func(key string) string { return env[key] }, func(context.Context, sofar.HardwareConfig) (domain.TelemetrySnapshot, error) {
		return domain.TelemetrySnapshot{}, errors.New("dial 10.0.0.5 using serial 123456789")
	})
	if err == nil {
		t.Fatal("run returned nil error")
	}
	for _, secret := range []string{"10.0.0.5", "123456789", "dial", "serial"} {
		if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(secret)) {
			t.Fatalf("error exposes %q: %v", secret, err)
		}
	}
}
