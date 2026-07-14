package sofar

import (
	"context"
	"fmt"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/solarman"
)

type ReaderConfig struct {
	SlaveID    byte
	ActiveMPPT map[int]bool
	Now        func() time.Time
}

type Reader struct {
	registers  solarman.RegisterReader
	slaveID    byte
	activeMPPT map[int]bool
	now        func() time.Time
}

func NewReader(registers solarman.RegisterReader, config ReaderConfig) *Reader {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	activeMPPT := make(map[int]bool, len(config.ActiveMPPT))
	for input, active := range config.ActiveMPPT {
		activeMPPT[input] = active
	}
	return &Reader{
		registers:  registers,
		slaveID:    config.SlaveID,
		activeMPPT: activeMPPT,
		now:        now,
	}
}

func (r *Reader) ReadSnapshot(ctx context.Context) (domain.TelemetrySnapshot, error) {
	blocks := snapshotBlocks()
	values := make([][]uint16, len(blocks))
	for i, block := range blocks {
		registers, err := r.registers.ReadHoldingRegisters(ctx, r.slaveID, block.start, block.count)
		if err != nil {
			return domain.TelemetrySnapshot{}, fmt.Errorf("read %s at 0x%04X: %w", block.name, block.start, err)
		}
		if len(registers) != int(block.count) {
			return domain.TelemetrySnapshot{}, fmt.Errorf("read %s at 0x%04X: got %d registers, want %d", block.name, block.start, len(registers), block.count)
		}
		values[i] = registers
	}

	statusFault, grid, pv, energy := values[0], values[1], values[2], values[3]
	snapshot := domain.TelemetrySnapshot{
		ObservedAt: r.now(),
		Status:     statusName(statusFault[statusOffset]),
		ACPowerW:   scaledSigned16(grid[acPowerOffset], 10),
		PV1: domain.MPPT{
			Active:   r.activeMPPT[1],
			VoltageV: scaledUnsigned16(pv[pv1VoltageOffset], 0.1),
			CurrentA: scaledUnsigned16(pv[pv1CurrentOffset], 0.01),
			PowerW:   scaledUnsigned16(pv[pv1PowerOffset], 10),
		},
		PV2: domain.MPPT{
			Active:   r.activeMPPT[2],
			VoltageV: scaledUnsigned16(pv[pv2VoltageOffset], 0.1),
			CurrentA: scaledUnsigned16(pv[pv2CurrentOffset], 0.01),
			PowerW:   scaledUnsigned16(pv[pv2PowerOffset], 10),
		},
		Grid: domain.Grid{
			VoltageV:    scaledUnsigned16(grid[gridVoltageOffset], 0.1),
			FrequencyHz: scaledUnsigned16(grid[gridFrequencyOffset], 0.01),
		},
		FaultCodes: decodeFaultCodes(statusFault[faultFirstOffset : faultFirstOffset+faultWordCount]),
	}

	// SOFAR stores each 32-bit counter low word first at the lower register.
	snapshot.EnergyTodayWh = float64(joinLowHigh(
		energy[energyTodayLowOffset], energy[energyTodayHighOffset],
	)) * 10
	snapshot.EnergyLifetimeWh = float64(joinLowHigh(
		energy[energyLifetimeLowOffset], energy[energyLifetimeHighOffset],
	)) * 100

	if err := validateSnapshot(snapshot); err != nil {
		return domain.TelemetrySnapshot{}, err
	}
	return snapshot, nil
}

func scaledUnsigned16(value uint16, scale float64) float64 {
	return float64(value) * scale
}

func scaledSigned16(value uint16, scale float64) float64 {
	return float64(int16(value)) * scale
}

func joinLowHigh(low, high uint16) uint32 {
	return uint32(high)<<16 | uint32(low)
}

func statusName(value uint16) string {
	statuses := [...]string{
		"waiting",
		"detection",
		"normal",
		"emergency_power",
		"fault",
		"permanent_fault",
		"upgrading",
		"self_charging",
	}
	if int(value) < len(statuses) {
		return statuses[value]
	}
	return fmt.Sprintf("unknown_%d", value)
}

func decodeFaultCodes(words []uint16) []uint16 {
	codes := make([]uint16, 0)
	for wordIndex, word := range words {
		for bit := 0; bit < 16; bit++ {
			if word&(uint16(1)<<bit) != 0 {
				codes = append(codes, uint16(wordIndex*16+bit+1))
			}
		}
	}
	return codes
}

func validateSnapshot(snapshot domain.TelemetrySnapshot) error {
	checks := []struct {
		name    string
		value   float64
		minimum float64
		maximum float64
	}{
		{name: "PV1 voltage", value: snapshot.PV1.VoltageV, minimum: 0, maximum: 600},
		{name: "PV1 current", value: snapshot.PV1.CurrentA, minimum: 0, maximum: 30},
		{name: "PV2 voltage", value: snapshot.PV2.VoltageV, minimum: 0, maximum: 600},
		{name: "PV2 current", value: snapshot.PV2.CurrentA, minimum: 0, maximum: 30},
		{name: "grid voltage", value: snapshot.Grid.VoltageV, minimum: 0, maximum: 600},
		{name: "grid frequency", value: snapshot.Grid.FrequencyHz, minimum: 40, maximum: 70},
		{name: "AC power", value: snapshot.ACPowerW, minimum: 0, maximum: 6600},
	}
	for _, check := range checks {
		if check.value < check.minimum || check.value > check.maximum {
			return fmt.Errorf("%s %.2f outside %.2f..%.2f", check.name, check.value, check.minimum, check.maximum)
		}
	}
	if snapshot.EnergyTodayWh < 0 {
		return fmt.Errorf("energy today %.2f must be nonnegative", snapshot.EnergyTodayWh)
	}
	if snapshot.EnergyLifetimeWh < snapshot.EnergyTodayWh {
		return fmt.Errorf("energy lifetime %.2f below energy today %.2f", snapshot.EnergyLifetimeWh, snapshot.EnergyTodayWh)
	}
	return nil
}
