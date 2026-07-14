package sofar

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestHardwareTestEnabledRequiresExactOptIn(t *testing.T) {
	for _, value := range []string{"", "0", "true", " 1", "1 "} {
		if HardwareTestEnabled(func(string) string { return value }) {
			t.Fatalf("HardwareTestEnabled(%q) = true, want false", value)
		}
	}
	if !HardwareTestEnabled(func(string) string { return "1" }) {
		t.Fatal("HardwareTestEnabled(1) = false, want true")
	}
}

func TestHardwareConfigFromEnv(t *testing.T) {
	valid := map[string]string{
		"HELIO_LOGGER_IP":     "10.0.0.5",
		"HELIO_LOGGER_SERIAL": "1234",
		"HELIO_MODBUS_SLAVE":  "1",
	}

	t.Run("accepts private target and default port", func(t *testing.T) {
		cfg, err := HardwareConfigFromLookup(mapLookup(valid))
		if err != nil {
			t.Fatalf("HardwareConfigFromLookup: %v", err)
		}
		if cfg.Address != "10.0.0.5:8899" || cfg.Serial != 1234 || cfg.SlaveID != 1 {
			t.Fatalf("config = %#v", cfg)
		}
	})

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing IP", key: "HELIO_LOGGER_IP", value: ""},
		{name: "URL scheme", key: "HELIO_LOGGER_IP", value: "tcp://10.0.0.5"},
		{name: "hostname", key: "HELIO_LOGGER_IP", value: "logger.local"},
		{name: "public IP", key: "HELIO_LOGGER_IP", value: "203.0.113.9"},
		{name: "serial overflow", key: "HELIO_LOGGER_SERIAL", value: "429496729" + "6"},
		{name: "serial negative", key: "HELIO_LOGGER_SERIAL", value: "-1"},
		{name: "slave zero", key: "HELIO_MODBUS_SLAVE", value: "0"},
		{name: "slave high", key: "HELIO_MODBUS_SLAVE", value: "248"},
		{name: "port zero", key: "HELIO_LOGGER_PORT", value: "0"},
		{name: "port high", key: "HELIO_LOGGER_PORT", value: "65536"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := cloneEnv(valid)
			env[test.key] = test.value
			_, err := HardwareConfigFromLookup(mapLookup(env))
			if err == nil {
				t.Fatal("HardwareConfigFromLookup returned nil error")
			}
			if strings.Contains(err.Error(), test.value) && test.value != "" {
				t.Fatalf("error exposes rejected value: %q", err)
			}
		})
	}
}

func TestHardwareConfigAllowsNonPrivateOnlyWithExactOptIn(t *testing.T) {
	env := map[string]string{
		"HELIO_LOGGER_IP":                "203.0.113.9",
		"HELIO_LOGGER_SERIAL":            "1234",
		"HELIO_MODBUS_SLAVE":             "1",
		"HELIO_ALLOW_NON_PRIVATE_LOGGER": "1",
	}
	if _, err := HardwareConfigFromLookup(mapLookup(env)); err != nil {
		t.Fatalf("HardwareConfigFromLookup: %v", err)
	}
	env["HELIO_ALLOW_NON_PRIVATE_LOGGER"] = "true"
	if _, err := HardwareConfigFromLookup(mapLookup(env)); err == nil {
		t.Fatal("non-private target accepted without exact opt-in")
	}
}

func TestMarshalHardwareSnapshotContainsTelemetryOnly(t *testing.T) {
	cfg := HardwareConfig{Address: "10.0.0.5:8899", Serial: 123456789, SlaveID: 1}
	snapshot := domain.TelemetrySnapshot{
		ObservedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		Status:     "normal",
		ACPowerW:   450,
	}
	encoded, err := MarshalHardwareSnapshot(snapshot)
	if err != nil {
		t.Fatalf("MarshalHardwareSnapshot: %v", err)
	}
	var decoded domain.TelemetrySnapshot
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.Status != snapshot.Status || !decoded.ObservedAt.Equal(snapshot.ObservedAt) {
		t.Fatalf("decoded snapshot = %#v", decoded)
	}
	for _, secret := range []string{cfg.Address, "123456789", "serial", "address", "raw"} {
		if strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(secret)) {
			t.Fatalf("output exposes %q: %s", secret, encoded)
		}
	}
}

func TestHardwareReadOnly(t *testing.T) {
	if os.Getenv("HELIO_HARDWARE_TEST") != "1" {
		t.Skip("set HELIO_HARDWARE_TEST=1")
	}
	cfg, err := HardwareConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewHardwareReader(cfg).ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ObservedAt.IsZero() {
		t.Fatal("missing observation time")
	}
}

func mapLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func cloneEnv(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
