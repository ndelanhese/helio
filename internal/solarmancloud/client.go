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
	"strings"
	"time"
)

const InternationalEndpoint = "https://globalapi.solarmanpv.com"

type Credentials struct {
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
	Account   string `json:"account"`
	Password  string `json:"password"`
}

type Station struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type Client struct {
	baseURL string
	http    *http.Client
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
