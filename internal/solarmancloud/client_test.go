package solarmancloud

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestTestReadsCurrentStationListID(t *testing.T) {
	client := New("https://example.test", &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.Path, "/account/") {
			return response(`{"success":true,"access_token":"token"}`), nil
		}
		return response(`{"success":true,"stationList":[{"id":1234,"name":"Casa"}]}`), nil
	})})
	stations, err := client.Test(context.Background(), Credentials{AppID: "app", AppSecret: "secret", Account: "user@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stations) != 1 || stations[0].ID != 1234 || stations[0].Name != "Casa" {
		t.Fatalf("stations = %#v", stations)
	}
}

func response(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
