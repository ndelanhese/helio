package sofar

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type fixtureBlock struct {
	Start  uint16   `json:"start"`
	Values []uint16 `json:"values"`
}

type decoderFixture struct {
	Blocks   []fixtureBlock           `json:"blocks"`
	Expected domain.TelemetrySnapshot `json:"expected"`
}

type readCall struct {
	Slave byte
	Start uint16
	Count uint16
}

type fixtureReader struct {
	blocks map[uint16][]uint16
	calls  []readCall
	err    error
}

func (r *fixtureReader) ReadHoldingRegisters(_ context.Context, slave byte, start, count uint16) ([]uint16, error) {
	r.calls = append(r.calls, readCall{Slave: slave, Start: start, Count: count})
	if r.err != nil {
		return nil, r.err
	}
	values, ok := r.blocks[start]
	if !ok {
		return nil, errors.New("unexpected register block")
	}
	return slices.Clone(values), nil
}

func TestReadSnapshotFromFixtures(t *testing.T) {
	observedAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"normal_day", "pv2_inactive", "fault"} {
		t.Run(name, func(t *testing.T) {
			fixture := loadFixture(t, name)
			registers := fixtureReaderFrom(fixture)
			reader := NewReader(&registers, ReaderConfig{
				SlaveID:    1,
				ActiveMPPT: map[int]bool{1: true},
				Now:        func() time.Time { return observedAt },
			})

			got, err := reader.ReadSnapshot(context.Background())
			if err != nil {
				t.Fatalf("ReadSnapshot: %v", err)
			}
			assertSnapshot(t, got, fixture.Expected)
			if !got.ObservedAt.Equal(observedAt) {
				t.Fatalf("ObservedAt = %v, want %v", got.ObservedAt, observedAt)
			}
			wantCalls := []readCall{
				{Slave: 1, Start: 0x0404, Count: 13},
				{Slave: 1, Start: 0x0484, Count: 10},
				{Slave: 1, Start: 0x0584, Count: 6},
				{Slave: 1, Start: 0x0684, Count: 4},
			}
			if !slices.Equal(registers.calls, wantCalls) {
				t.Fatalf("read calls = %#v, want %#v", registers.calls, wantCalls)
			}
		})
	}
}

func TestReadSnapshotPreservesInactiveMPPTValues(t *testing.T) {
	fixture := loadFixture(t, "normal_day")
	fixture.Blocks[2].Values[3] = 1200
	fixture.Blocks[2].Values[4] = 125
	fixture.Blocks[2].Values[5] = 15
	registers := fixtureReaderFrom(fixture)
	reader := NewReader(&registers, ReaderConfig{ActiveMPPT: map[int]bool{1: true}})

	got, err := reader.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.PV2.Active || !closeEnough(got.PV2.VoltageV, 120) || !closeEnough(got.PV2.CurrentA, 1.25) || !closeEnough(got.PV2.PowerW, 150) {
		t.Fatalf("PV2 = %+v, want inactive with decoded values", got.PV2)
	}
}

func TestReadSnapshotDecodesBigRegisterWordOrder(t *testing.T) {
	fixture := loadFixture(t, "normal_day")
	fixture.Blocks[3].Values = []uint16{2, 1, 3, 2}
	registers := fixtureReaderFrom(fixture)
	reader := NewReader(&registers, ReaderConfig{})

	got, err := reader.ReadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := float64(uint32(2)<<16|1) * 10; !closeEnough(got.EnergyTodayWh, want) {
		t.Fatalf("EnergyTodayWh = %v, want %v", got.EnergyTodayWh, want)
	}
	if want := float64(uint32(3)<<16|2) * 100; !closeEnough(got.EnergyLifetimeWh, want) {
		t.Fatalf("EnergyLifetimeWh = %v, want %v", got.EnergyLifetimeWh, want)
	}
}

func TestReadSnapshotRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name      string
		block     int
		offset    int
		value     uint16
		wantField string
	}{
		{name: "negative signed AC power", block: 1, offset: 1, value: 0xffff, wantField: "AC power"},
		{name: "AC power above inverter limit", block: 1, offset: 1, value: 661, wantField: "AC power"},
		{name: "low grid frequency", block: 1, offset: 0, value: 3999, wantField: "grid frequency"},
		{name: "high grid frequency", block: 1, offset: 0, value: 7001, wantField: "grid frequency"},
		{name: "high grid voltage", block: 1, offset: 9, value: 6001, wantField: "grid voltage"},
		{name: "high PV voltage", block: 2, offset: 0, value: 6001, wantField: "PV1 voltage"},
		{name: "high PV current", block: 2, offset: 1, value: 3001, wantField: "PV1 current"},
		{name: "high inactive PV voltage", block: 2, offset: 3, value: 6001, wantField: "PV2 voltage"},
		{name: "high inactive PV current", block: 2, offset: 4, value: 3001, wantField: "PV2 current"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := loadFixture(t, "normal_day")
			fixture.Blocks[tt.block].Values[tt.offset] = tt.value
			registers := fixtureReaderFrom(fixture)
			reader := NewReader(&registers, ReaderConfig{})

			_, err := reader.ReadSnapshot(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantField) {
				t.Fatalf("error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestReadSnapshotRejectsEnergyLifetimeBelowToday(t *testing.T) {
	fixture := loadFixture(t, "normal_day")
	fixture.Blocks[3].Values = []uint16{200, 0, 1, 0}
	registers := fixtureReaderFrom(fixture)
	reader := NewReader(&registers, ReaderConfig{})

	_, err := reader.ReadSnapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "energy lifetime") {
		t.Fatalf("error = %v, want energy lifetime error", err)
	}
}

func TestReadSnapshotReportsReadAndLengthErrors(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		registers := fixtureReader{err: errors.New("offline")}
		_, err := NewReader(&registers, ReaderConfig{}).ReadSnapshot(context.Background())
		if err == nil || !strings.Contains(err.Error(), "status/fault block") || !errors.Is(err, registers.err) {
			t.Fatalf("error = %v, want wrapped block error", err)
		}
	})

	t.Run("short block", func(t *testing.T) {
		fixture := loadFixture(t, "normal_day")
		fixture.Blocks[0].Values = fixture.Blocks[0].Values[:1]
		registers := fixtureReaderFrom(fixture)
		_, err := NewReader(&registers, ReaderConfig{}).ReadSnapshot(context.Background())
		if err == nil || !strings.Contains(err.Error(), "status/fault block") {
			t.Fatalf("error = %v, want block length error", err)
		}
	})
}

func loadFixture(t *testing.T, name string) decoderFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture decoderFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func fixtureReaderFrom(fixture decoderFixture) fixtureReader {
	blocks := make(map[uint16][]uint16, len(fixture.Blocks))
	for _, block := range fixture.Blocks {
		blocks[block.Start] = block.Values
	}
	return fixtureReader{blocks: blocks}
}

func assertSnapshot(t *testing.T, got, want domain.TelemetrySnapshot) {
	t.Helper()
	if got.Status != want.Status || !slices.Equal(got.FaultCodes, want.FaultCodes) {
		t.Errorf("status/faults = %q/%v, want %q/%v", got.Status, got.FaultCodes, want.Status, want.FaultCodes)
	}
	for name, values := range map[string][2]float64{
		"AC power":        {got.ACPowerW, want.ACPowerW},
		"energy today":    {got.EnergyTodayWh, want.EnergyTodayWh},
		"energy lifetime": {got.EnergyLifetimeWh, want.EnergyLifetimeWh},
		"PV1 voltage":     {got.PV1.VoltageV, want.PV1.VoltageV},
		"PV1 current":     {got.PV1.CurrentA, want.PV1.CurrentA},
		"PV1 power":       {got.PV1.PowerW, want.PV1.PowerW},
		"grid voltage":    {got.Grid.VoltageV, want.Grid.VoltageV},
		"grid frequency":  {got.Grid.FrequencyHz, want.Grid.FrequencyHz},
	} {
		if !closeEnough(values[0], values[1]) {
			t.Errorf("%s = %v, want %v", name, values[0], values[1])
		}
	}
	if got.PV1.Active != want.PV1.Active || got.PV2.Active != want.PV2.Active {
		t.Errorf("MPPT active = %v/%v, want %v/%v", got.PV1.Active, got.PV2.Active, want.PV1.Active, want.PV2.Active)
	}
}

func closeEnough(got, want float64) bool {
	return math.Abs(got-want) <= 0.01
}
