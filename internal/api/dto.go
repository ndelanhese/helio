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
	FlagChargeMinor      *json.Number `json:"flagChargeMinor"`
}

type manualTariffDTO struct {
	Distributor                  *string      `json:"distributor"`
	EffectiveFrom                *string      `json:"effectiveFrom"`
	EffectiveTo                  *string      `json:"effectiveTo"`
	ConsumptionTEMicrosPerKWh    *json.Number `json:"consumptionTEMicrosPerKWh"`
	ConsumptionTUSDMicrosPerKWh  *json.Number `json:"consumptionTUSDMicrosPerKWh"`
	CompensationTEMicrosPerKWh   *json.Number `json:"compensationTEMicrosPerKWh"`
	CompensationTUSDMicrosPerKWh *json.Number `json:"compensationTUSDMicrosPerKWh"`
	FlagMicrosPerKWh             *json.Number `json:"flagMicrosPerKWh"`
	AvailabilityKWh              *json.Number `json:"availabilityKWh"`
	CIPMinor                     *json.Number `json:"cipMinor"`
}

func (d manualTariffDTO) domain(now time.Time) (domain.TariffProposal, error) {
	if d.Distributor == nil || d.EffectiveFrom == nil || d.EffectiveTo == nil || d.ConsumptionTEMicrosPerKWh == nil || d.ConsumptionTUSDMicrosPerKWh == nil || d.CompensationTEMicrosPerKWh == nil || d.CompensationTUSDMicrosPerKWh == nil || d.FlagMicrosPerKWh == nil || d.AvailabilityKWh == nil || d.CIPMinor == nil {
		return domain.TariffProposal{}, errors.New("all manual tariff fields are required")
	}
	from, err := time.Parse("2006-01-02", *d.EffectiveFrom)
	if err != nil {
		return domain.TariffProposal{}, errors.New("effectiveFrom must be a civil date")
	}
	to, err := time.Parse("2006-01-02", *d.EffectiveTo)
	if err != nil {
		return domain.TariffProposal{}, errors.New("effectiveTo must be a civil date")
	}
	parse := func(value *json.Number) (int64, error) { return strconv.ParseInt(value.String(), 10, 64) }
	te, err := parse(d.ConsumptionTEMicrosPerKWh)
	if err != nil {
		return domain.TariffProposal{}, errors.New("consumption TE must be an integer")
	}
	tusd, err := parse(d.ConsumptionTUSDMicrosPerKWh)
	if err != nil {
		return domain.TariffProposal{}, errors.New("consumption TUSD must be an integer")
	}
	compTE, err := parse(d.CompensationTEMicrosPerKWh)
	if err != nil {
		return domain.TariffProposal{}, errors.New("compensation TE must be an integer")
	}
	compTUSD, err := parse(d.CompensationTUSDMicrosPerKWh)
	if err != nil {
		return domain.TariffProposal{}, errors.New("compensation TUSD must be an integer")
	}
	flag, err := parse(d.FlagMicrosPerKWh)
	if err != nil {
		return domain.TariffProposal{}, errors.New("flag must be an integer")
	}
	availability, err := parse(d.AvailabilityKWh)
	if err != nil {
		return domain.TariffProposal{}, errors.New("availability must be an integer")
	}
	cip, err := parse(d.CIPMinor)
	if err != nil {
		return domain.TariffProposal{}, errors.New("CIP must be an integer")
	}
	return domain.TariffProposal{Distributor: *d.Distributor, EffectiveFrom: from.UTC(), EffectiveTo: to.UTC().Add(24*time.Hour - time.Nanosecond), ConsumptionTEMicrosPerKWh: te, ConsumptionTUSDMicrosPerKWh: tusd, CompensationTEMicrosPerKWh: compTE, CompensationTUSDMicrosPerKWh: compTUSD, FlagMicrosPerKWh: flag, AvailabilityKWh: int(availability), CIPMinor: cip, SourceURL: "/finance", ParserVersion: "manual-bill-v1", RetrievedAt: now.UTC()}, nil
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

func (d billingCycleDTO) domain(location *time.Location) (domain.BillingCycle, error) {
	if d.ReadingStart == nil || d.ReadingEnd == nil || d.ActiveConsumptionKWh == nil || d.InjectedKWh == nil || d.CreditsUsedKWh == nil || d.CreditBalanceKWh == nil || d.TotalPaidMinor == nil || d.FlagChargeMinor == nil {
		return domain.BillingCycle{}, errors.New("all billing cycle fields are required")
	}
	parseDate := func(value string) (time.Time, error) {
		if civil, err := time.ParseInLocation("2006-01-02", value, location); err == nil {
			return civil, nil
		}
		return time.Parse(time.RFC3339, value)
	}
	start, err := parseDate(*d.ReadingStart)
	if err != nil {
		return domain.BillingCycle{}, errors.New("readingStart must be a civil date or RFC3339")
	}
	end, err := parseDate(*d.ReadingEnd)
	if err != nil {
		return domain.BillingCycle{}, errors.New("readingEnd must be a civil date or RFC3339")
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
	flag, err := parse("flagChargeMinor", d.FlagChargeMinor)
	if err != nil {
		return domain.BillingCycle{}, err
	}
	cycle := domain.BillingCycle{ReadingStart: start, ReadingEnd: end, ActiveConsumptionKWh: active, InjectedKWh: injected, CreditsUsedKWh: used, CreditBalanceKWh: balance, TotalPaidMinor: paid, FlagChargeMinor: flag}
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
