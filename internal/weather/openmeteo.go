package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	openMeteoTimeout = 5 * time.Second
	maxForecastDays  = 7
	maxResponseBytes = 1 << 20
	maxIrradianceWM2 = 2000
)

type OpenMeteo struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

func NewOpenMeteo(baseURL string, client *http.Client, clock func() time.Time) *OpenMeteo {
	if client == nil {
		client = http.DefaultClient
	}
	if clock == nil {
		clock = time.Now
	}
	return &OpenMeteo{baseURL: strings.TrimRight(baseURL, "/"), client: client, now: clock}
}

func (p *OpenMeteo) Source() string { return "open-meteo" }

func (p *OpenMeteo) Current(ctx context.Context, request Request) (Current, error) {
	if !finiteInRange(request.Latitude, -90, 90) || !finiteInRange(request.Longitude, -180, 180) {
		return Current{}, errors.New("weather request coordinates are invalid")
	}
	if _, err := url.ParseRequestURI(p.baseURL); err != nil {
		return Current{}, errors.New("weather provider URL is invalid")
	}
	query := url.Values{}
	query.Set("latitude", strconv.FormatFloat(request.Latitude, 'f', -1, 64))
	query.Set("longitude", strconv.FormatFloat(request.Longitude, 'f', -1, 64))
	query.Set("current", "temperature_2m,precipitation,weather_code,cloud_cover,wind_speed_10m,shortwave_radiation")
	query.Set("timezone", "UTC")

	timedCtx, cancel := context.WithTimeout(ctx, openMeteoTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(timedCtx, http.MethodGet, p.baseURL+"?"+query.Encode(), nil)
	if err != nil {
		return Current{}, errors.New("build weather request")
	}
	httpRequest.Header.Set("Accept", "application/json")
	response, err := p.client.Do(httpRequest)
	if err != nil {
		return Current{}, errors.New("weather provider request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Current{}, fmt.Errorf("weather provider returned status class %dxx", response.StatusCode/100)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Current{}, errors.New("weather provider returned unsupported content type")
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return Current{}, errors.New("read weather provider response")
	}
	if len(body) > maxResponseBytes {
		return Current{}, errors.New("weather provider response is too large")
	}
	var payload openMeteoResponse
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return Current{}, errors.New("weather provider returned invalid JSON")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Current{}, errors.New("weather provider returned invalid JSON")
	}
	if payload.Current == nil {
		return Current{}, errors.New("weather provider returned missing current conditions")
	}
	at, err := time.Parse("2006-01-02T15:04", payload.Current.Time)
	if err != nil || !finiteInRange(payload.Current.TemperatureC, -100, 100) || !finiteInRange(payload.Current.PrecipitationMM, 0, 1_000) || payload.Current.WeatherCode < 0 || payload.Current.WeatherCode > 99 || !finiteInRange(payload.Current.CloudCoverPct, 0, 100) || !finiteInRange(payload.Current.WindSpeedKMH, 0, 500) || !finiteInRange(payload.Current.IrradianceWM2, 0, maxIrradianceWM2) {
		return Current{}, errors.New("weather provider returned invalid current conditions")
	}
	return Current{At: at.UTC(), TemperatureC: payload.Current.TemperatureC, PrecipitationMM: payload.Current.PrecipitationMM, WeatherCode: payload.Current.WeatherCode, CloudCoverPct: payload.Current.CloudCoverPct, WindSpeedKMH: payload.Current.WindSpeedKMH, IrradianceWM2: payload.Current.IrradianceWM2}, nil
}

func (p *OpenMeteo) Hourly(ctx context.Context, request Request) ([]Hour, error) {
	start := request.Start.UTC()
	end := request.End.UTC()
	if request.Start.IsZero() {
		start = p.now().UTC()
	}
	if request.End.IsZero() {
		end = start
	}
	if end.Before(start) || end.Sub(start) > maxForecastDays*24*time.Hour {
		return nil, errors.New("weather request date range is invalid")
	}
	if !finiteInRange(request.Latitude, -90, 90) || !finiteInRange(request.Longitude, -180, 180) {
		return nil, errors.New("weather request coordinates are invalid")
	}
	if _, err := url.ParseRequestURI(p.baseURL); err != nil {
		return nil, errors.New("weather provider URL is invalid")
	}
	query := url.Values{}
	query.Set("latitude", strconv.FormatFloat(request.Latitude, 'f', -1, 64))
	query.Set("longitude", strconv.FormatFloat(request.Longitude, 'f', -1, 64))
	query.Set("hourly", "cloud_cover,shortwave_radiation")
	query.Set("timezone", "UTC")
	query.Set("start_date", start.Format("2006-01-02"))
	query.Set("end_date", end.Format("2006-01-02"))

	timedCtx, cancel := context.WithTimeout(ctx, openMeteoTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(timedCtx, http.MethodGet, p.baseURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, errors.New("build weather request")
	}
	httpRequest.Header.Set("Accept", "application/json")
	response, err := p.client.Do(httpRequest)
	if err != nil {
		return nil, errors.New("weather provider request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("weather provider returned status class %dxx", response.StatusCode/100)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, errors.New("weather provider returned unsupported content type")
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, errors.New("read weather provider response")
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("weather provider response is too large")
	}
	var payload openMeteoResponse
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("weather provider returned invalid JSON")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, errors.New("weather provider returned invalid JSON")
	}
	if len(payload.Hourly.Time) != len(payload.Hourly.CloudCover) || len(payload.Hourly.Time) != len(payload.Hourly.ShortwaveRadiation) {
		return nil, errors.New("weather provider returned inconsistent hourly data")
	}
	hours := make([]Hour, 0, len(payload.Hourly.Time))
	var previous time.Time
	for i, rawTime := range payload.Hourly.Time {
		at, err := time.Parse("2006-01-02T15:04", rawTime)
		if err != nil {
			return nil, errors.New("weather provider returned invalid hourly timestamp")
		}
		at = at.UTC()
		if at.Minute() != 0 || at.Second() != 0 || at.Nanosecond() != 0 || (!previous.IsZero() && !at.After(previous)) {
			return nil, errors.New("weather provider returned unordered hourly timestamps")
		}
		previous = at
		if payload.Hourly.CloudCover[i] == nil || payload.Hourly.ShortwaveRadiation[i] == nil {
			return nil, errors.New("weather provider returned missing hourly values")
		}
		cloud := *payload.Hourly.CloudCover[i]
		irradiance := *payload.Hourly.ShortwaveRadiation[i]
		if !finiteInRange(cloud, 0, 100) || !finiteInRange(irradiance, 0, maxIrradianceWM2) {
			return nil, errors.New("weather provider returned invalid hourly values")
		}
		if !at.Before(start) && at.Before(end) {
			hours = append(hours, Hour{Time: at, CloudCoverPct: cloud, IrradianceWM2: irradiance})
		}
	}
	return hours, nil
}

func finiteInRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("additional JSON value")
	}
	return nil
}

type openMeteoResponse struct {
	Latitude             float64                `json:"latitude,omitempty"`
	Longitude            float64                `json:"longitude,omitempty"`
	GenerationTimeMS     float64                `json:"generationtime_ms,omitempty"`
	UTCOffsetSeconds     int                    `json:"utc_offset_seconds,omitempty"`
	Timezone             string                 `json:"timezone,omitempty"`
	TimezoneAbbreviation string                 `json:"timezone_abbreviation,omitempty"`
	Elevation            float64                `json:"elevation,omitempty"`
	HourlyUnits          *openMeteoHourlyUnits  `json:"hourly_units,omitempty"`
	Hourly               openMeteoHourly        `json:"hourly"`
	CurrentUnits         *openMeteoCurrentUnits `json:"current_units,omitempty"`
	Current              *openMeteoCurrent      `json:"current,omitempty"`
}
type openMeteoCurrentUnits struct {
	Time            string `json:"time"`
	Interval        string `json:"interval"`
	TemperatureC    string `json:"temperature_2m"`
	PrecipitationMM string `json:"precipitation"`
	WeatherCode     string `json:"weather_code"`
	CloudCoverPct   string `json:"cloud_cover"`
	WindSpeedKMH    string `json:"wind_speed_10m"`
	IrradianceWM2   string `json:"shortwave_radiation"`
}
type openMeteoCurrent struct {
	Time            string  `json:"time"`
	Interval        int     `json:"interval"`
	TemperatureC    float64 `json:"temperature_2m"`
	PrecipitationMM float64 `json:"precipitation"`
	WeatherCode     int     `json:"weather_code"`
	CloudCoverPct   float64 `json:"cloud_cover"`
	WindSpeedKMH    float64 `json:"wind_speed_10m"`
	IrradianceWM2   float64 `json:"shortwave_radiation"`
}
type openMeteoHourlyUnits struct {
	Time               string `json:"time"`
	CloudCover         string `json:"cloud_cover"`
	ShortwaveRadiation string `json:"shortwave_radiation"`
}
type openMeteoHourly struct {
	Time               []string   `json:"time"`
	CloudCover         []*float64 `json:"cloud_cover"`
	ShortwaveRadiation []*float64 `json:"shortwave_radiation"`
}
