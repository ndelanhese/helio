package domain

// Settings contains the normalized configuration needed to communicate with
// the logger and interpret the installation's production.
type Settings struct {
	LoggerHost        string  `json:"loggerHost"`
	LoggerSerial      string  `json:"loggerSerial"`
	LoggerPort        int     `json:"loggerPort"`
	ModbusSlave       int     `json:"modbusSlave"`
	PanelCount        int     `json:"panelCount"`
	PanelWattage      int     `json:"panelWattage"`
	ActiveMPPT        []int   `json:"activeMPPT"`
	InstalledPowerW   int     `json:"installedPowerW"`
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
	Timezone          string  `json:"timezone"`
	Currency          string  `json:"currency"`
	TariffMinorPerKWh int64   `json:"tariffMinorPerKWh"`
	RetentionDays     int     `json:"retentionDays"`
}
