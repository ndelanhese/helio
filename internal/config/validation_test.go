package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/domain"
)

func validSettings() domain.Settings {
	return domain.Settings{
		LoggerHost: "192.168.1.50", LoggerPort: 8899, LoggerSerial: "1234567890",
		ModbusSlave: 1, PanelCount: 7, PanelWattage: 610, ActiveMPPT: []int{1},
		Latitude: -23.5505, Longitude: -46.6333, Timezone: "America/Sao_Paulo",
		Currency: "BRL", TariffMinorPerKWh: 95, RetentionDays: 730,
	}
}

func TestSettingsValidFixtureIsNormalized(t *testing.T) {
	in := validSettings()
	in.LoggerSerial = "0001234567"
	in.ActiveMPPT = []int{2, 1}
	in.InstalledPowerW = 1 // a client-provided derived value must never win

	got, err := config.ValidateSettings(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.LoggerSerial != "1234567" {
		t.Fatalf("serial = %q", got.LoggerSerial)
	}
	if !reflect.DeepEqual(got.ActiveMPPT, []int{1, 2}) {
		t.Fatalf("MPPT = %v", got.ActiveMPPT)
	}
	if got.InstalledPowerW != 4270 {
		t.Fatalf("installed power = %d", got.InstalledPowerW)
	}
}

func TestSettingsRetentionDefaultsTo730Days(t *testing.T) {
	in := validSettings()
	in.RetentionDays = 0
	got, err := config.ValidateSettings(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.RetentionDays != 730 {
		t.Fatalf("retention = %d", got.RetentionDays)
	}
}

func TestSettingsConnectionDefaults(t *testing.T) {
	in := validSettings()
	in.LoggerPort = 0
	in.ModbusSlave = 0
	got, err := config.ValidateSettings(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.LoggerPort != 8899 || got.ModbusSlave != 1 {
		t.Fatalf("port=%d slave=%d", got.LoggerPort, got.ModbusSlave)
	}
}

func TestSettingsHostPolicy(t *testing.T) {
	for _, host := range []string{"https://192.168.1.50", "logger.local", "8.8.8.8"} {
		t.Run(host, func(t *testing.T) {
			in := validSettings()
			in.LoggerHost = host
			if _, err := config.ValidateSettings(in); err == nil {
				t.Fatal("expected host rejection")
			}
		})
	}
	t.Run("explicit public override", func(t *testing.T) {
		in := validSettings()
		in.LoggerHost = "8.8.8.8"
		if _, err := config.ValidateSettings(in, true); err != nil {
			t.Fatal(err)
		}
	})
	for _, host := range []string{"127.0.0.1", "169.254.10.2"} {
		t.Run(host, func(t *testing.T) {
			in := validSettings()
			in.LoggerHost = host
			if _, err := config.ValidateSettings(in); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSettingsValidationMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.Settings)
	}{
		{"serial nondecimal", func(s *domain.Settings) { s.LoggerSerial = "12a" }},
		{"serial signed", func(s *domain.Settings) { s.LoggerSerial = "+123" }},
		{"serial uint32 overflow", func(s *domain.Settings) { s.LoggerSerial = "4294967296" }},
		{"port negative", func(s *domain.Settings) { s.LoggerPort = -1 }},
		{"port overflow", func(s *domain.Settings) { s.LoggerPort = 65536 }},
		{"slave negative", func(s *domain.Settings) { s.ModbusSlave = -1 }},
		{"slave overflow", func(s *domain.Settings) { s.ModbusSlave = 248 }},
		{"duplicate MPPT", func(s *domain.Settings) { s.ActiveMPPT = []int{1, 1} }},
		{"unknown MPPT", func(s *domain.Settings) { s.ActiveMPPT = []int{3} }},
		{"no MPPT", func(s *domain.Settings) { s.ActiveMPPT = nil }},
		{"capacity over 12 kWp", func(s *domain.Settings) { s.PanelCount = 20 }},
		{"capacity multiplication overflow", func(s *domain.Settings) { s.PanelCount = int(^uint(0) >> 1); s.PanelWattage = int(^uint(0) >> 1) }},
		{"zero panels", func(s *domain.Settings) { s.PanelCount = 0 }},
		{"zero panel wattage", func(s *domain.Settings) { s.PanelWattage = 0 }},
		{"latitude", func(s *domain.Settings) { s.Latitude = 91 }},
		{"longitude", func(s *domain.Settings) { s.Longitude = -181 }},
		{"timezone", func(s *domain.Settings) { s.Timezone = "Sao Paulo" }},
		{"timezone empty", func(s *domain.Settings) { s.Timezone = "" }},
		{"timezone local", func(s *domain.Settings) { s.Timezone = "Local" }},
		{"currency case", func(s *domain.Settings) { s.Currency = "brl" }},
		{"currency unknown", func(s *domain.Settings) { s.Currency = "ZZZ" }},
		{"tariff negative", func(s *domain.Settings) { s.TariffMinorPerKWh = -1 }},
		{"retention low", func(s *domain.Settings) { s.RetentionDays = 29 }},
		{"retention high", func(s *domain.Settings) { s.RetentionDays = 3651 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := validSettings()
			tc.mutate(&in)
			if _, err := config.ValidateSettings(in); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDecodeSettingsJSONRejectsUnknownAndDerivedFields(t *testing.T) {
	base := `{"loggerHost":"192.168.1.50","loggerSerial":"123","loggerPort":8899,"modbusSlave":1,"panelCount":7,"panelWattage":610,"activeMPPT":[1],"latitude":-23.5,"longitude":-46.6,"timezone":"America/Sao_Paulo","currency":"BRL","tariffMinorPerKWh":95,"retentionDays":730}`
	for _, extra := range []string{`,"unknown":1`, `,"installedPowerW":1`} {
		payload := strings.TrimSuffix(base, "}") + extra + "}"
		if _, err := config.DecodeSettingsJSON(strings.NewReader(payload)); err == nil {
			t.Fatal("expected strict decoding error")
		}
	}
}
