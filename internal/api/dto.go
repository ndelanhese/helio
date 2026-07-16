package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

const maxRequestBody = 64 << 10

type settingsDTO struct {
	LoggerHost        *string  `json:"loggerHost"`
	LoggerSerial      *string  `json:"loggerSerial"`
	LoggerPort        *int     `json:"loggerPort"`
	ModbusSlave       *int     `json:"modbusSlave"`
	PanelCount        *int     `json:"panelCount"`
	PanelWattage      *int     `json:"panelWattage"`
	ActiveMPPT        *[]int   `json:"activeMPPT"`
	Latitude          *float64 `json:"latitude"`
	Longitude         *float64 `json:"longitude"`
	Timezone          *string  `json:"timezone"`
	Currency          *string  `json:"currency"`
	TariffMinorPerKWh *int64   `json:"tariffMinorPerKWh"`
	RetentionDays     *int     `json:"retentionDays"`
}

type billingCycleDTO struct {
	ReadingStart         *string      `json:"readingStart"`
	ReadingEnd           *string      `json:"readingEnd"`
	ActiveConsumptionKWh *json.Number `json:"activeConsumptionKWh"`
	InjectedKWh          *json.Number `json:"injectedKWh"`
	CreditsUsedKWh       *json.Number `json:"creditsUsedKWh"`
	CreditBalanceKWh     *json.Number `json:"creditBalanceKWh"`
	TotalPaidMinor       *json.Number `json:"totalPaidMinor"`
}

func decodeBillingCycle(w http.ResponseWriter, r *http.Request, body *billingCycleDTO) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(body); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return errors.New("request body must contain one JSON value")
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return err
	}
	return nil
}

func (d billingCycleDTO) domain() (domain.BillingCycle, error) {
	if d.ReadingStart == nil || d.ReadingEnd == nil || d.ActiveConsumptionKWh == nil || d.InjectedKWh == nil || d.CreditsUsedKWh == nil || d.CreditBalanceKWh == nil || d.TotalPaidMinor == nil {
		return domain.BillingCycle{}, errors.New("all billing cycle fields are required")
	}
	start, err := time.Parse(time.RFC3339, *d.ReadingStart)
	if err != nil {
		return domain.BillingCycle{}, errors.New("readingStart must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, *d.ReadingEnd)
	if err != nil {
		return domain.BillingCycle{}, errors.New("readingEnd must be RFC3339")
	}
	parse := func(name string, value *json.Number) (int64, error) {
		parsed, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil || parsed < 0 {
			return 0, fmt.Errorf("%s must be a nonnegative integer", name)
		}
		return parsed, nil
	}
	active, err := parse("activeConsumptionKWh", d.ActiveConsumptionKWh)
	if err != nil {
		return domain.BillingCycle{}, err
	}
	injected, err := parse("injectedKWh", d.InjectedKWh)
	if err != nil {
		return domain.BillingCycle{}, err
	}
	used, err := parse("creditsUsedKWh", d.CreditsUsedKWh)
	if err != nil {
		return domain.BillingCycle{}, err
	}
	balance, err := parse("creditBalanceKWh", d.CreditBalanceKWh)
	if err != nil {
		return domain.BillingCycle{}, err
	}
	paid, err := parse("totalPaidMinor", d.TotalPaidMinor)
	if err != nil {
		return domain.BillingCycle{}, err
	}
	cycle := domain.BillingCycle{ReadingStart: start, ReadingEnd: end, ActiveConsumptionKWh: active, InjectedKWh: injected, CreditsUsedKWh: used, CreditBalanceKWh: balance, TotalPaidMinor: paid}
	if err := domain.ValidateBillingCycle(cycle); err != nil {
		return domain.BillingCycle{}, err
	}
	return cycle, nil
}

func (d settingsDTO) domain() (domain.Settings, error) {
	missing := ""
	for name, present := range map[string]bool{
		"loggerHost": d.LoggerHost != nil, "loggerSerial": d.LoggerSerial != nil,
		"panelCount": d.PanelCount != nil, "panelWattage": d.PanelWattage != nil,
		"activeMPPT": d.ActiveMPPT != nil, "latitude": d.Latitude != nil,
		"longitude": d.Longitude != nil, "timezone": d.Timezone != nil, "currency": d.Currency != nil,
	} {
		if !present {
			missing = name
			break
		}
	}
	if missing != "" {
		return domain.Settings{}, fmt.Errorf("%s is required", missing)
	}
	if d.LoggerPort != nil && *d.LoggerPort == 0 {
		return domain.Settings{}, errors.New("loggerPort must not be zero when provided")
	}
	if d.ModbusSlave != nil && *d.ModbusSlave == 0 {
		return domain.Settings{}, errors.New("modbusSlave must not be zero when provided")
	}
	if d.RetentionDays != nil && *d.RetentionDays == 0 {
		return domain.Settings{}, errors.New("retentionDays must not be zero when provided")
	}
	s := domain.Settings{LoggerHost: *d.LoggerHost, LoggerSerial: *d.LoggerSerial, PanelCount: *d.PanelCount,
		PanelWattage: *d.PanelWattage, ActiveMPPT: append([]int(nil), (*d.ActiveMPPT)...), Latitude: *d.Latitude,
		Longitude: *d.Longitude, Timezone: *d.Timezone, Currency: *d.Currency}
	if d.LoggerPort != nil {
		s.LoggerPort = *d.LoggerPort
	}
	if d.ModbusSlave != nil {
		s.ModbusSlave = *d.ModbusSlave
	}
	if d.TariffMinorPerKWh != nil {
		s.TariffMinorPerKWh = *d.TariffMinorPerKWh
	}
	if d.RetentionDays != nil {
		s.RetentionDays = *d.RetentionDays
	}
	return s, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 64 KiB")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain one JSON value")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
