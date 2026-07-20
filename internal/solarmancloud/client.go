package solarmancloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const InternationalEndpoint = "https://globalapi.solarmanpv.com"

type Credentials struct {
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
	Account   string `json:"account"`
	Password  string `json:"password"`
}

// FetchFrames returns historical power frames. Solarman accepts exactly one
// calendar day per frame query; callers should keep ranges short.
func (c *Client) FetchFrames(ctx context.Context, credentials Credentials, stationID int64, from, to time.Time) ([]Frame, error) {
	if stationID <= 0 {
		return nil, errors.New("Solarman station is required")
	}
	if from.After(to) {
		return nil, errors.New("invalid sync range")
	}
	if to.Sub(from) > 29*24*time.Hour {
		return nil, errors.New("sync range must not exceed 30 days")
	}
	token, err := c.token(ctx, credentials)
	if err != nil {
		return nil, err
	}
	frames := []Frame{}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if err := c.waitTurn(ctx); err != nil {
			return nil, err
		}
		body, _ := json.Marshal(map[string]any{"stationId": stationID, "timeType": 1, "startTime": day.Format("2006-01-02"), "endTime": day.Format("2006-01-02")})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/station/v1.0/history?language=en", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "bearer "+token)
		response, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Solarman history: %w", err)
		}
		var payload struct {
			Success bool             `json:"success"`
			Msg     string           `json:"msg"`
			Items   []map[string]any `json:"stationDataItems"`
		}
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil {
			return nil, errors.New("Solarman returned invalid historical data")
		}
		if !payload.Success {
			return nil, solarmanError(payload.Msg)
		}
		for _, item := range payload.Items {
			if frame, ok := decodeFrame(item, day.Location()); ok {
				frames = append(frames, frame)
			}
		}
	}
	return frames, nil
}

type Frame struct {
	At     time.Time
	PowerW float64
}

func decodeFrame(value map[string]any, location *time.Location) (Frame, bool) {
	power, ok := number(value["generationPower"])
	if !ok {
		return Frame{}, false
	}
	raw, ok := value["dateTime"]
	if !ok {
		return Frame{}, false
	}
	var at time.Time
	switch v := raw.(type) {
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if parsed, err := time.ParseInLocation(layout, v, location); err == nil {
				at = parsed
				break
			}
		}
	case float64:
		at = time.Unix(int64(v), 0)
	}
	if at.IsZero() {
		return Frame{}, false
	}
	return Frame{At: at.UTC(), PowerW: power}, true
}
func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

type Station struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

const requestsPerMinute = 40

type Client struct {
	baseURL string
	http    *http.Client
	mu      sync.Mutex
	next    time.Time
}

func New(baseURL string, client *http.Client) *Client {
	if baseURL == "" {
		baseURL = InternationalEndpoint
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: client}
}

func (c *Client) Test(ctx context.Context, credentials Credentials) ([]Station, error) {
	token, err := c.token(ctx, credentials)
	if err != nil {
		return nil, err
	}
	if err := c.waitTurn(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/station/v1.0/list?language=en", bytes.NewReader([]byte(`{"page":1,"size":50}`)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+token)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Solarman station list: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("Solarman station list returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Success     bool   `json:"success"`
		Msg         string `json:"msg"`
		StationList []struct {
			ID        int64  `json:"id"`
			StationID int64  `json:"stationId"`
			Name      string `json:"name"`
		} `json:"stationList"`
		Stations []struct {
			ID        int64  `json:"id"`
			StationID int64  `json:"stationId"`
			Name      string `json:"name"`
		} `json:"stations"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, errors.New("Solarman returned an invalid station list")
	}
	if !payload.Success {
		return nil, solarmanError(payload.Msg)
	}
	entries := payload.StationList
	if len(entries) == 0 {
		entries = payload.Stations
	}
	stations := make([]Station, 0, len(entries))
	for _, entry := range entries {
		id := entry.ID
		if id == 0 {
			id = entry.StationID
		}
		if id != 0 {
			stations = append(stations, Station{ID: id, Name: entry.Name})
		}
	}
	return stations, nil
}

func (c *Client) waitTurn(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	when := c.next
	interval := time.Minute / requestsPerMinute
	if when.Before(now) {
		when = now
	}
	c.next = when.Add(interval)
	c.mu.Unlock()
	if delay := time.Until(when); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (c *Client) token(ctx context.Context, credentials Credentials) (string, error) {
	if strings.TrimSpace(credentials.AppID) == "" || strings.TrimSpace(credentials.AppSecret) == "" || strings.TrimSpace(credentials.Account) == "" || credentials.Password == "" {
		return "", errors.New("app ID, app secret, account, and password are required")
	}
	digest := sha256.Sum256([]byte(credentials.Password))
	login := map[string]string{"appSecret": credentials.AppSecret, "password": hex.EncodeToString(digest[:])}
	if strings.Contains(credentials.Account, "@") {
		login["email"] = credentials.Account
	} else {
		login["username"] = credentials.Account
	}
	body, err := json.Marshal(login)
	if err != nil {
		return "", err
	}
	endpoint := c.baseURL + "/account/v1.0/token?" + url.Values{"appId": []string{credentials.AppID}, "language": []string{"en"}}.Encode()
	if err := c.waitTurn(ctx); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("Solarman authentication: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("Solarman authentication returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Success     bool   `json:"success"`
		Msg         string `json:"msg"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", errors.New("Solarman returned an invalid authentication response")
	}
	if !payload.Success || payload.AccessToken == "" {
		return "", solarmanError(payload.Msg)
	}
	return payload.AccessToken, nil
}

func solarmanError(message string) error {
	if message == "" {
		message = "credentials were not accepted"
	}
	return fmt.Errorf("Solarman: %s", message)
}
