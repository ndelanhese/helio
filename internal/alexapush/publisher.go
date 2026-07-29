package alexapush

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
)

const defaultInterval = time.Minute

type Config struct {
	Endpoint string
	Secret   string
	Interval time.Duration
	Client   *http.Client
}

type Publisher struct {
	events      <-chan collector.Event
	unsubscribe func()
	endpoint    string
	secret      []byte
	interval    time.Duration
	client      *http.Client
}

type snapshotPayload struct {
	Version       int       `json:"version"`
	ObservedAt    time.Time `json:"observedAt"`
	ACPowerW      float64   `json:"acPowerW"`
	EnergyTodayWh float64   `json:"energyTodayWh"`
	Status        string    `json:"status"`
	Stale         bool      `json:"stale"`
}

func New(hub *collector.Hub, config Config) (*Publisher, error) {
	if config.Endpoint == "" && config.Secret == "" {
		return nil, nil
	}
	if hub == nil || config.Endpoint == "" || config.Secret == "" {
		return nil, errors.New("alexa publisher requires hub, relay URL, and shared secret")
	}
	endpoint, err := validateEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	secret, err := base64.StdEncoding.DecodeString(config.Secret)
	if err != nil || len(secret) < 32 {
		return nil, errors.New("alexa shared secret must be base64-encoded and at least 32 bytes")
	}
	interval := config.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	events, unsubscribe := hub.Subscribe()
	return &Publisher{events: events, unsubscribe: unsubscribe, endpoint: endpoint, secret: secret, interval: interval, client: client}, nil
}

func validateEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid Alexa relay URL")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	local := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && local) {
		return "", errors.New("Alexa relay URL must use HTTPS")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ingest"
	}
	return parsed.String(), nil
}

func (p *Publisher) Run(ctx context.Context) {
	if p == nil {
		return
	}
	defer p.unsubscribe()
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	var latest *snapshotPayload
	sent := false
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-p.events:
			if !ok {
				return
			}
			snapshot := event.Snapshot
			if snapshot == nil {
				snapshot = event.State.Snapshot
			}
			if snapshot == nil {
				continue
			}
			next := payload(snapshot, event.State.Stale)
			latest = &next
			if !sent {
				// First snapshot should reach a newly deployed relay immediately.
				if err := p.send(ctx, *latest); err != nil {
					log.Printf("alexa relay: %v", err)
					continue
				}
				latest = nil
				sent = true
			}
		case <-ticker.C:
			if latest == nil {
				continue
			}
			if err := p.send(ctx, *latest); err != nil {
				log.Printf("alexa relay: %v", err)
				continue
			}
			latest = nil
			sent = true
		}
	}
}

func payload(snapshot *domain.TelemetrySnapshot, stale bool) snapshotPayload {
	return snapshotPayload{
		Version: 1, ObservedAt: snapshot.ObservedAt.UTC(), ACPowerW: snapshot.ACPowerW,
		EnergyTodayWh: snapshot.EnergyTodayWh, Status: snapshot.Status, Stale: stale,
	}
}

func (p *Publisher) send(ctx context.Context, payload snapshotPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, p.secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "helio-alexa-publisher/1")
	request.Header.Set("X-Helio-Timestamp", timestamp)
	request.Header.Set("X-Helio-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("send snapshot: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("relay returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	return nil
}
