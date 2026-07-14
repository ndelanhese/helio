package config

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

const defaultRetentionDays = 730

// DecodeSettingsJSON decodes the onboarding request shape. Derived fields and
// unknown fields are deliberately not part of this API boundary.
func DecodeSettingsJSON(r io.Reader) (domain.Settings, error) {
	type onboardingSettings struct {
		LoggerHost        string  `json:"loggerHost"`
		LoggerSerial      string  `json:"loggerSerial"`
		LoggerPort        int     `json:"loggerPort"`
		ModbusSlave       int     `json:"modbusSlave"`
		PanelCount        int     `json:"panelCount"`
		PanelWattage      int     `json:"panelWattage"`
		ActiveMPPT        []int   `json:"activeMPPT"`
		Latitude          float64 `json:"latitude"`
		Longitude         float64 `json:"longitude"`
		Timezone          string  `json:"timezone"`
		Currency          string  `json:"currency"`
		TariffMinorPerKWh int64   `json:"tariffMinorPerKWh"`
		RetentionDays     int     `json:"retentionDays"`
	}
	var wire onboardingSettings
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return domain.Settings{}, fmt.Errorf("decode settings: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return domain.Settings{}, err
	}
	return domain.Settings{
		LoggerHost: wire.LoggerHost, LoggerSerial: wire.LoggerSerial,
		LoggerPort: wire.LoggerPort, ModbusSlave: wire.ModbusSlave,
		PanelCount: wire.PanelCount, PanelWattage: wire.PanelWattage,
		ActiveMPPT: wire.ActiveMPPT, Latitude: wire.Latitude,
		Longitude: wire.Longitude, Timezone: wire.Timezone,
		Currency: wire.Currency, TariffMinorPerKWh: wire.TariffMinorPerKWh,
		RetentionDays: wire.RetentionDays,
	}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode settings: multiple JSON values")
		}
		return fmt.Errorf("decode settings: %w", err)
	}
	return nil
}

// ValidateSettings validates and normalizes settings. Public logger IPs are
// accepted only when the process configuration explicitly passes true.
func ValidateSettings(in domain.Settings, allowPublicLogger ...bool) (domain.Settings, error) {
	ip := net.ParseIP(in.LoggerHost)
	if ip == nil || ip.To4() == nil {
		return domain.Settings{}, fmt.Errorf("logger host must be an IPv4 address")
	}
	allowPublic := len(allowPublicLogger) > 0 && allowPublicLogger[0]
	if !allowPublic && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
		return domain.Settings{}, fmt.Errorf("logger host must be private unless public access is explicitly enabled")
	}
	if in.LoggerPort < 1 || in.LoggerPort > 65535 {
		return domain.Settings{}, fmt.Errorf("logger port must be between 1 and 65535")
	}
	serial, err := strconv.ParseUint(in.LoggerSerial, 10, 32)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("logger serial must be a decimal uint32: %w", err)
	}
	if in.ModbusSlave < 1 || in.ModbusSlave > 247 {
		return domain.Settings{}, fmt.Errorf("modbus slave must be between 1 and 247")
	}
	if in.PanelCount <= 0 || in.PanelWattage <= 0 {
		return domain.Settings{}, fmt.Errorf("panel count and wattage must be positive")
	}
	if in.PanelCount > 12_000/in.PanelWattage {
		return domain.Settings{}, fmt.Errorf("installed power must not exceed 12000 W")
	}
	installed := in.PanelCount * in.PanelWattage
	mppt, err := normalizeMPPT(in.ActiveMPPT)
	if err != nil {
		return domain.Settings{}, err
	}
	if math.IsNaN(in.Latitude) || math.IsInf(in.Latitude, 0) || in.Latitude < -90 || in.Latitude > 90 {
		return domain.Settings{}, fmt.Errorf("latitude must be between -90 and 90")
	}
	if math.IsNaN(in.Longitude) || math.IsInf(in.Longitude, 0) || in.Longitude < -180 || in.Longitude > 180 {
		return domain.Settings{}, fmt.Errorf("longitude must be between -180 and 180")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return domain.Settings{}, fmt.Errorf("timezone must be a valid IANA location: %w", err)
	}
	if !validCurrency(in.Currency) {
		return domain.Settings{}, fmt.Errorf("currency must be an uppercase ISO 4217 code")
	}
	if in.TariffMinorPerKWh < 0 {
		return domain.Settings{}, fmt.Errorf("tariff must not be negative")
	}
	if in.RetentionDays == 0 {
		in.RetentionDays = defaultRetentionDays
	}
	if in.RetentionDays < 30 || in.RetentionDays > 3650 {
		return domain.Settings{}, fmt.Errorf("retention must be between 30 and 3650 days")
	}
	in.LoggerHost = ip.String()
	in.LoggerSerial = strconv.FormatUint(serial, 10)
	in.ActiveMPPT = mppt
	in.InstalledPowerW = installed
	return in, nil
}

func normalizeMPPT(inputs []int) ([]int, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("at least one MPPT input must be active")
	}
	normalized := append([]int(nil), inputs...)
	sort.Ints(normalized)
	for i, input := range normalized {
		if input < 1 || input > 2 {
			return nil, fmt.Errorf("unknown MPPT input %d", input)
		}
		if i > 0 && normalized[i-1] == input {
			return nil, fmt.Errorf("duplicate MPPT input %d", input)
		}
	}
	return normalized, nil
}

func validCurrency(currency string) bool {
	if len(currency) != 3 || currency != strings.ToUpper(currency) {
		return false
	}
	const iso4217 = " AED AFN ALL AMD ANG AOA ARS AUD AWG AZN BAM BBD BDT BGN BHD BIF BMD BND BOB BOV BRL BSD BTN BWP BYN BZD CAD CDF CHE CHF CHW CLF CLP CNY COP COU CRC CUC CUP CVE CZK DJF DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP GMD GNF GTQ GYD HKD HNL HRK HTG HUF IDR ILS INR IQD IRR ISK JMD JOD JPY KES KGS KHR KMF KPW KRW KWD KYD KZT LAK LBP LKR LRD LSL LYD MAD MDL MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MXV MYR MZN NAD NGN NIO NOK NPR NZD OMR PAB PEN PGK PHP PKR PLN PYG QAR RON RSD RUB RWF SAR SBD SCR SDG SEK SGD SHP SLE SLL SOS SRD SSP STN SVC SYP SZL THB TJS TMT TND TOP TRY TTD TWD TZS UAH UGX USD USN UYI UYU UYW UZS VED VES VND VUV WST XAF XAG XAU XBA XBB XBC XBD XCD XDR XOF XPD XPF XPT XSU XTS XUA XXX YER ZAR ZMW ZWL "
	return strings.Contains(iso4217, " "+currency+" ")
}
