package sofar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/solarman"
)

const defaultHardwarePort = 8899

type HardwareConfig struct {
	Address    string
	Serial     uint32
	SlaveID    byte
	ActiveMPPT []int
}

type HardwareReader struct {
	reader *Reader
}

func HardwareTestEnabled(lookup func(string) string) bool {
	return lookup("HELIO_HARDWARE_TEST") == "1"
}

func HardwareConfigFromEnv() (HardwareConfig, error) {
	return HardwareConfigFromLookup(os.Getenv)
}

func HardwareConfigFromLookup(lookup func(string) string) (HardwareConfig, error) {
	ip := net.ParseIP(lookup("HELIO_LOGGER_IP"))
	if ip == nil {
		return HardwareConfig{}, errors.New("HELIO_LOGGER_IP must be an IP address without a URL scheme")
	}
	if !ip.IsPrivate() && lookup("HELIO_ALLOW_NON_PRIVATE_LOGGER") != "1" {
		return HardwareConfig{}, errors.New("HELIO_LOGGER_IP must be private unless explicitly allowed")
	}

	serial, err := strconv.ParseUint(lookup("HELIO_LOGGER_SERIAL"), 10, 32)
	if err != nil {
		return HardwareConfig{}, errors.New("HELIO_LOGGER_SERIAL must be a uint32")
	}
	slave, err := parseBoundedUint(lookup("HELIO_MODBUS_SLAVE"), 1, 247, "HELIO_MODBUS_SLAVE")
	if err != nil {
		return HardwareConfig{}, err
	}

	port := uint64(defaultHardwarePort)
	if value := lookup("HELIO_LOGGER_PORT"); value != "" {
		port, err = parseBoundedUint(value, 1, 65535, "HELIO_LOGGER_PORT")
		if err != nil {
			return HardwareConfig{}, err
		}
	}

	return HardwareConfig{
		Address: net.JoinHostPort(ip.String(), strconv.FormatUint(port, 10)),
		Serial:  uint32(serial),
		SlaveID: byte(slave),
	}, nil
}

func parseBoundedUint(value string, minimum, maximum uint64, name string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}

func NewHardwareReader(config HardwareConfig) *HardwareReader {
	activeMPPT := map[int]bool{1: true, 2: true}
	if len(config.ActiveMPPT) > 0 {
		activeMPPT = make(map[int]bool, len(config.ActiveMPPT))
		for _, input := range config.ActiveMPPT {
			activeMPPT[input] = true
		}
	}
	client := solarman.NewClient(solarman.Config{
		Address: config.Address,
		Serial:  config.Serial,
		Timeout: 5 * time.Second,
	}, nil)
	return &HardwareReader{reader: NewReader(client, ReaderConfig{
		SlaveID:    config.SlaveID,
		ActiveMPPT: activeMPPT,
	})}
}

func (r *HardwareReader) ReadSnapshot(ctx context.Context) (domain.TelemetrySnapshot, error) {
	snapshot, err := r.reader.ReadSnapshot(ctx)
	if err != nil {
		return domain.TelemetrySnapshot{}, errors.New("hardware read failed")
	}
	return snapshot, nil
}

func MarshalHardwareSnapshot(snapshot domain.TelemetrySnapshot) ([]byte, error) {
	return json.Marshal(snapshot)
}
