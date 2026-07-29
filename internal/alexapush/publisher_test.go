package alexapush

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
)

func TestPublisherSendsSignedSanitizedSnapshot(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	received := make(chan snapshotPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		timestamp := r.Header.Get("X-Helio-Timestamp")
		if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil {
			t.Errorf("timestamp=%q", timestamp)
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(timestamp + "."))
		_, _ = mac.Write(body)
		if got, want := r.Header.Get("X-Helio-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); !hmac.Equal([]byte(got), []byte(want)) {
			t.Errorf("signature=%q want %q", got, want)
		}
		var payload snapshotPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(string(body), "faultCodes") || strings.Contains(string(body), "grid") || strings.Contains(string(body), "energyLifetime") {
			t.Errorf("payload leaked extra telemetry: %s", body)
		}
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	hub := collector.NewHub()
	publisher, err := New(hub, Config{Endpoint: server.URL + "/ingest", Secret: base64.StdEncoding.EncodeToString(secret), Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go publisher.Run(ctx)

	observed := time.Date(2026, 7, 29, 18, 30, 0, 0, time.UTC)
	hub.Publish(collector.Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{
		ObservedAt: observed, ACPowerW: 2840, EnergyTodayWh: 14200, EnergyLifetimeWh: 900000,
		Status: "normal", FaultCodes: []uint16{7}, Grid: domain.Grid{VoltageV: 230},
	}, State: collector.State{Stale: false}})

	select {
	case got := <-received:
		if got.Version != 1 || !got.ObservedAt.Equal(observed) || got.ACPowerW != 2840 || got.EnergyTodayWh != 14200 || got.Status != "normal" || got.Stale {
			t.Fatalf("payload=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot was not sent")
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	hub := collector.NewHub()
	secret := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []Config{
		{Endpoint: "http://example.com/ingest", Secret: secret},
		{Endpoint: "https://example.com/ingest?secret=bad", Secret: secret},
		{Endpoint: "https://example.com/ingest", Secret: "short"},
		{Endpoint: "https://example.com/ingest"},
	} {
		if publisher, err := New(hub, test); err == nil || publisher != nil {
			t.Fatalf("config=%+v publisher=%v err=%v", test, publisher, err)
		}
	}
}
