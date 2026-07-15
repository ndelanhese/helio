package weather

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenMeteoMapsHourlyUTCAndExactQuery(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		want := map[string]string{
			"latitude": "-23.5505", "longitude": "-46.6333",
			"hourly": "cloud_cover,shortwave_radiation", "timezone": "UTC",
			"start_date": "2024-06-21", "end_date": "2024-06-22",
		}
		for key, value := range want {
			if q.Get(key) != value {
				t.Errorf("query %s = %q, want %q", key, q.Get(key), value)
			}
		}
		if len(q) != len(want) {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"hourly":{"time":["2024-06-21T12:00","2024-06-21T13:00"],"cloud_cover":[25,30],"shortwave_radiation":[510.5,480]}}`)
	}))
	defer server.Close()

	provider := NewOpenMeteo(server.URL, server.Client(), func() time.Time { return time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC) })
	hours, err := provider.Hourly(context.Background(), Request{Latitude: -23.5505, Longitude: -46.6333, Start: time.Date(2024, 6, 21, 5, 0, 0, 0, time.FixedZone("local", -3*3600)), End: time.Date(2024, 6, 22, 5, 0, 0, 0, time.FixedZone("local", -3*3600))})
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 2 || !hours[0].Time.Equal(time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)) || hours[0].CloudCoverPct != 25 || hours[0].IrradianceWM2 != 510.5 {
		t.Fatalf("mapped hours = %#v", hours)
	}
}

func TestOpenMeteoMapsCurrentConditions(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got, want := q.Get("current"), "temperature_2m,precipitation,weather_code,cloud_cover,wind_speed_10m,shortwave_radiation"; got != want {
			t.Errorf("current = %q, want %q", got, want)
		}
		if q.Get("latitude") != "-23.5505" || q.Get("longitude") != "-46.6333" || q.Get("timezone") != "UTC" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"current_units":{"time":"iso8601","interval":"seconds","temperature_2m":"°C","precipitation":"mm","weather_code":"wmo code","cloud_cover":"%","wind_speed_10m":"km/h","shortwave_radiation":"W/m²"},"current":{"time":"2024-06-21T12:15","interval":900,"temperature_2m":22.4,"precipitation":0.3,"weather_code":61,"cloud_cover":78,"wind_speed_10m":14.2,"shortwave_radiation":645.8}}`)
	}))
	defer server.Close()

	current, err := NewOpenMeteo(server.URL, server.Client(), time.Now).Current(context.Background(), Request{Latitude: -23.5505, Longitude: -46.6333})
	if err != nil {
		t.Fatal(err)
	}
	if !current.At.Equal(time.Date(2024, 6, 21, 12, 15, 0, 0, time.UTC)) || current.TemperatureC != 22.4 || current.PrecipitationMM != 0.3 || current.WeatherCode != 61 || current.CloudCoverPct != 78 || current.WindSpeedKMH != 14.2 || current.IrradianceWM2 != 645.8 {
		t.Fatalf("current = %#v", current)
	}
}

func TestOpenMeteoRejectsUnsafeResponsesAndBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, contentType, body string
		status                  int
	}{
		{"status", "application/json", `{}`, http.StatusBadGateway},
		{"content type", "text/html", `{}`, http.StatusOK},
		{"unknown JSON", "application/json", `{"hourly":{"time":[],"cloud_cover":[],"shortwave_radiation":[]},"extra":1}`, http.StatusOK},
		{"mismatched arrays", "application/json", `{"hourly":{"time":["2024-06-21T12:00"],"cloud_cover":[],"shortwave_radiation":[1]}}`, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			p := NewOpenMeteo(server.URL, server.Client(), time.Now)
			_, err := p.Hourly(context.Background(), Request{Start: time.Now().UTC(), End: time.Now().UTC()})
			if err == nil || strings.Contains(err.Error(), tt.body) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	p := NewOpenMeteo("https://example.invalid", http.DefaultClient, time.Now)
	_, err := p.Hourly(context.Background(), Request{Start: time.Now().UTC(), End: time.Now().UTC().AddDate(0, 0, 8)})
	if err == nil {
		t.Fatal("expected bounded date error")
	}
}

func TestOpenMeteoAppliesFiveSecondTimeout(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) < 4*time.Second {
			t.Fatalf("deadline = %v, ok=%v", deadline, ok)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"hourly":{"time":[],"cloud_cover":[],"shortwave_radiation":[]}}`))}, nil
	})}
	p := NewOpenMeteo("https://weather.invalid", client, time.Now)
	if _, err := p.Hourly(context.Background(), Request{Start: time.Now().UTC(), End: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenMeteoUsesInjectedClockForOmittedDateBounds(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 6, 21, 23, 30, 0, 0, time.FixedZone("BRT", -3*3600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("start_date"); got != "2024-06-22" {
			t.Fatalf("start_date = %q", got)
		}
		if got := r.URL.Query().Get("end_date"); got != "2024-06-22" {
			t.Fatalf("end_date = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"hourly":{"time":[],"cloud_cover":[],"shortwave_radiation":[]}}`)
	}))
	defer server.Close()
	p := NewOpenMeteo(server.URL, server.Client(), func() time.Time { return now })
	if _, err := p.Hourly(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenMeteoFiltersFullUTCDatesToRequestedLocalDay(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"hourly":{"time":["2024-06-21T02:00","2024-06-21T03:00","2024-06-21T04:00","2024-06-22T02:00","2024-06-22T03:00"],"cloud_cover":[1,2,3,4,5],"shortwave_radiation":[0,10,20,30,40]}}`)
	}))
	defer server.Close()
	brt := time.FixedZone("BRT", -3*60*60)
	start := time.Date(2024, 6, 21, 0, 0, 0, 0, brt)
	end := start.Add(24 * time.Hour)
	hours, err := NewOpenMeteo(server.URL, server.Client(), time.Now).Hourly(context.Background(), Request{Start: start, End: end})
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 3 || !hours[0].Time.Equal(start.UTC()) || !hours[2].Time.Equal(end.UTC().Add(-time.Hour)) {
		t.Fatalf("filtered hours=%+v", hours)
	}
}

func TestOpenMeteoRejectsInvalidHourlyValuesAndOrdering(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, body string }{
		{"null cloud", `{"hourly":{"time":["2024-06-21T12:00"],"cloud_cover":[null],"shortwave_radiation":[1]}}`},
		{"null irradiance", `{"hourly":{"time":["2024-06-21T12:00"],"cloud_cover":[1],"shortwave_radiation":[null]}}`},
		{"cloud below zero", `{"hourly":{"time":["2024-06-21T12:00"],"cloud_cover":[-1],"shortwave_radiation":[1]}}`},
		{"cloud above 100", `{"hourly":{"time":["2024-06-21T12:00"],"cloud_cover":[101],"shortwave_radiation":[1]}}`},
		{"irradiance below zero", `{"hourly":{"time":["2024-06-21T12:00"],"cloud_cover":[1],"shortwave_radiation":[-1]}}`},
		{"irradiance implausible", `{"hourly":{"time":["2024-06-21T12:00"],"cloud_cover":[1],"shortwave_radiation":[2001]}}`},
		{"duplicate timestamp", `{"hourly":{"time":["2024-06-21T12:00","2024-06-21T12:00"],"cloud_cover":[1,2],"shortwave_radiation":[1,2]}}`},
		{"descending timestamp", `{"hourly":{"time":["2024-06-21T13:00","2024-06-21T12:00"],"cloud_cover":[1,2],"shortwave_radiation":[1,2]}}`},
		{"non-hour timestamp", `{"hourly":{"time":["2024-06-21T12:30"],"cloud_cover":[1],"shortwave_radiation":[1]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			_, err := NewOpenMeteo(server.URL, server.Client(), time.Now).Hourly(context.Background(), Request{Start: time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC), End: time.Date(2024, 6, 21, 14, 0, 0, 0, time.UTC)})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestOpenMeteoRejectsOversizedAndTrailingJSON(t *testing.T) {
	t.Parallel()
	tests := []string{
		`{"hourly":{"time":[],"cloud_cover":[],"shortwave_radiation":[]}} {}`,
		`{"hourly":{"time":[],"cloud_cover":[],"shortwave_radiation":[]},"padding":"` + strings.Repeat("x", maxResponseBytes) + `"}`,
	}
	for _, body := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}))
		_, err := NewOpenMeteo(server.URL, server.Client(), time.Now).Hourly(context.Background(), Request{})
		server.Close()
		if err == nil {
			t.Fatal("expected bounded strict JSON error")
		}
	}
}

func TestOpenMeteoHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewOpenMeteo(server.URL, server.Client(), time.Now).Hourly(ctx, Request{})
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error class")
		}
	case <-time.After(time.Second):
		t.Fatal("provider ignored caller cancellation")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
