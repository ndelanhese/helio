package alerts

import (
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

var ruleBase = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

func TestRuleBoundaries(t *testing.T) {
	config := DefaultConfig()
	tests := []struct {
		name  string
		rule  Rule
		state State
		input Input
		open  bool
	}{
		{"logger waits for third failure", LoggerOfflineRule(), State{Consecutive: 1}, Input{At: ruleBase, PollObserved: true}, false},
		{"logger opens on third failure", LoggerOfflineRule(), State{Consecutive: 2}, Input{At: ruleBase, PollObserved: true}, true},
		{"stale opens exactly at threshold", TelemetryStaleRule(), State{}, Input{At: ruleBase, LastTelemetryAt: ruleBase.Add(-config.StaleAfter)}, true},
		{"zero rejects elevation at ten", ZeroSunnyGenerationRule(), State{PendingSince: ruleBase.Add(-config.ZeroGenerationFor)}, sunnyInput(ruleBase, 10, 200, 80), false},
		{"zero accepts just above ten", ZeroSunnyGenerationRule(), State{PendingSince: ruleBase.Add(-config.ZeroGenerationFor)}, sunnyInput(ruleBase, 10.01, 200, 80), true},
		{"zero rejects irradiance below boundary", ZeroSunnyGenerationRule(), State{PendingSince: ruleBase.Add(-config.ZeroGenerationFor)}, sunnyInput(ruleBase, 11, 199.99, 80), false},
		{"zero rejects coverage below boundary", ZeroSunnyGenerationRule(), State{PendingSince: ruleBase.Add(-config.ZeroGenerationFor)}, sunnyInput(ruleBase, 11, 200, 79.99), false},
		{"zero waits below duration", ZeroSunnyGenerationRule(), State{PendingSince: ruleBase.Add(-config.ZeroGenerationFor + time.Nanosecond)}, sunnyInput(ruleBase, 11, 200, 80), false},
		{"zero opens exactly at duration", ZeroSunnyGenerationRule(), State{PendingSince: ruleBase.Add(-config.ZeroGenerationFor)}, sunnyInput(ruleBase, 11, 200, 80), true},
		{"voltage waits below duration", GridVoltageRule(), State{PendingSince: ruleBase.Add(-config.VoltageFor + time.Nanosecond)}, telemetryInput(ruleBase, 201, 60), false},
		{"voltage opens exactly at duration", GridVoltageRule(), State{PendingSince: ruleBase.Add(-config.VoltageFor)}, telemetryInput(ruleBase, 201, 60), true},
		{"voltage lower boundary is allowed", GridVoltageRule(), State{PendingSince: ruleBase.Add(-config.VoltageFor)}, telemetryInput(ruleBase, 202, 60), false},
		{"voltage upper boundary is allowed", GridVoltageRule(), State{PendingSince: ruleBase.Add(-config.VoltageFor)}, telemetryInput(ruleBase, 240, 60), false},
		{"frequency waits below duration", GridFrequencyRule(), State{PendingSince: ruleBase.Add(-config.FrequencyFor + time.Nanosecond)}, telemetryInput(ruleBase, 220, 59), false},
		{"frequency opens exactly at duration", GridFrequencyRule(), State{PendingSince: ruleBase.Add(-config.FrequencyFor)}, telemetryInput(ruleBase, 220, 59), true},
		{"frequency lower boundary is allowed", GridFrequencyRule(), State{PendingSince: ruleBase.Add(-config.FrequencyFor)}, telemetryInput(ruleBase, 220, 59.5), false},
		{"frequency upper boundary is allowed", GridFrequencyRule(), State{PendingSince: ruleBase.Add(-config.FrequencyFor)}, telemetryInput(ruleBase, 220, 60.5), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := test.rule.Evaluate(test.input, test.state, false, config)
			if decision.ShouldOpen != test.open {
				t.Fatalf("ShouldOpen=%v, want %v; next=%+v", decision.ShouldOpen, test.open, decision.Next)
			}
		})
	}
}

func TestRuleHysteresisAndSuppression(t *testing.T) {
	config := DefaultConfig()

	fault := InverterFaultRule().Evaluate(Input{At: ruleBase, TelemetryObserved: true, TelemetryFresh: true, PV1Fault: true}, State{}, false, config)
	if !fault.ShouldOpen {
		t.Fatal("fresh PV1 fault did not open immediately")
	}
	firstClear := InverterFaultRule().Evaluate(Input{At: ruleBase.Add(time.Second), TelemetryObserved: true, TelemetryFresh: true}, State{}, true, config)
	if firstClear.ShouldResolve || firstClear.Next.Consecutive != 1 {
		t.Fatalf("first clear=%+v", firstClear)
	}
	secondClear := InverterFaultRule().Evaluate(Input{At: ruleBase.Add(2 * time.Second), TelemetryObserved: true, TelemetryFresh: true}, firstClear.Next, true, config)
	if !secondClear.ShouldResolve {
		t.Fatal("second fresh clear did not resolve")
	}

	pv2 := InverterFaultRule().Evaluate(Input{At: ruleBase, TelemetryObserved: true, TelemetryFresh: true, PV2Fault: true, PV2Active: false}, State{}, false, config)
	if pv2.ShouldOpen {
		t.Fatal("inactive PV2 opened a fault")
	}

	for name, mutate := range map[string]func(*Input){
		"night":               func(in *Input) { in.SolarElevationDeg = -1 },
		"weather unavailable": func(in *Input) { in.WeatherAvailable = false },
		"fresh fault":         func(in *Input) { in.PV1Fault = true },
	} {
		t.Run(name, func(t *testing.T) {
			in := sunnyInput(ruleBase, 11, 200, 80)
			mutate(&in)
			decision := ZeroSunnyGenerationRule().Evaluate(in, State{PendingSince: ruleBase.Add(-time.Hour)}, false, config)
			if decision.ShouldOpen || !decision.Next.PendingSince.IsZero() {
				t.Fatalf("suppression failed: %+v", decision)
			}
		})
	}
}

func TestRuleDiscontinuitiesResetPendingEvidence(t *testing.T) {
	config := DefaultConfig()

	logger := LoggerOfflineRule().Evaluate(Input{At: ruleBase, PollObserved: true, PollSucceeded: true}, State{Consecutive: 2, PendingSince: ruleBase.Add(-time.Minute)}, false, config)
	if logger.Next.Consecutive != 0 || !logger.Next.PendingSince.IsZero() {
		t.Fatalf("successful poll retained failure evidence: %+v", logger.Next)
	}

	zero := ZeroSunnyGenerationRule().Evaluate(sunnyInput(ruleBase, 9, 200, 80), State{PendingSince: ruleBase.Add(-19 * time.Minute)}, false, config)
	if !zero.Next.PendingSince.IsZero() {
		t.Fatalf("night sample retained zero-generation window: %+v", zero.Next)
	}
	restarted := ZeroSunnyGenerationRule().Evaluate(sunnyInput(ruleBase.Add(time.Minute), 11, 200, 80), zero.Next, false, config)
	if !restarted.Next.PendingSince.Equal(ruleBase.Add(time.Minute)) || restarted.ShouldOpen {
		t.Fatalf("zero-generation window did not restart: %+v", restarted)
	}

	voltage := GridVoltageRule().Evaluate(telemetryInput(ruleBase, 220, 60), State{PendingSince: ruleBase.Add(-4 * time.Minute)}, false, config)
	if !voltage.Next.PendingSince.IsZero() {
		t.Fatalf("normal voltage retained duration evidence: %+v", voltage.Next)
	}

	nonFreshClear := InverterFaultRule().Evaluate(Input{At: ruleBase, TelemetryObserved: true, TelemetryFresh: false}, State{Consecutive: 1}, true, config)
	if nonFreshClear.ShouldResolve || nonFreshClear.Next.Consecutive != 1 {
		t.Fatalf("non-fresh sample counted toward fault recovery: %+v", nonFreshClear)
	}

	nonconsecutive := PersistentUnderproductionRule().Evaluate(analysisInput(ruleBase.AddDate(0, 0, 3), .20), State{Consecutive: 2, LastKey: ruleBase.Format("2006-01-02")}, false, config)
	if nonconsecutive.ShouldOpen || nonconsecutive.Next.Consecutive != 1 {
		t.Fatalf("day gap retained underproduction streak: %+v", nonconsecutive.Next)
	}
}

func TestStableRuleOrderReturnsCopy(t *testing.T) {
	first := StableRuleOrder()
	first[0] = "modified"
	second := StableRuleOrder()
	if second[0] != RuleLoggerOffline {
		t.Fatalf("stable rule order was mutable: %v", second)
	}
}

func TestRuleUnderproductionQualifyingDayHysteresis(t *testing.T) {
	config := DefaultConfig()
	rule := PersistentUnderproductionRule()
	state := State{}
	for day := 1; day <= 3; day++ {
		in := analysisInput(ruleBase.AddDate(0, 0, day), 0.64)
		decision := rule.Evaluate(in, state, false, config)
		state = decision.Next
		if decision.ShouldOpen != (day == 3) {
			t.Fatalf("day %d open=%v", day, decision.ShouldOpen)
		}
	}
	duplicate := rule.Evaluate(analysisInput(ruleBase.AddDate(0, 0, 3), 0.64), state, false, config)
	if duplicate.Next.Consecutive != 0 {
		t.Fatalf("duplicate day incremented streak: %+v", duplicate.Next)
	}
	state = State{}
	for day := 4; day <= 5; day++ {
		decision := rule.Evaluate(analysisInput(ruleBase.AddDate(0, 0, day), 0.80), state, true, config)
		state = decision.Next
		if decision.ShouldResolve != (day == 5) {
			t.Fatalf("recovery day %d resolve=%v", day, decision.ShouldResolve)
		}
	}

	notQualifying := analysisInput(ruleBase.AddDate(0, 0, 6), 0.20)
	notQualifying.Analysis.Qualifying = false
	if got := rule.Evaluate(notQualifying, State{Consecutive: 2}, false, config); got.ShouldOpen || got.Next.Consecutive != 0 {
		t.Fatalf("nonqualifying analysis retained evidence: %+v", got)
	}
	noWeather := analysisInput(ruleBase.AddDate(0, 0, 7), 0.20)
	noWeather.WeatherAvailable = false
	if got := rule.Evaluate(noWeather, State{Consecutive: 2}, false, config); got.ShouldOpen || got.Next.Consecutive != 0 {
		t.Fatalf("missing weather retained evidence: %+v", got)
	}
}

func TestRuleUnderproductionRecoveryStartsAfterOpening(t *testing.T) {
	config := DefaultConfig()
	rule := PersistentUnderproductionRule()
	state := State{}
	for day := 1; day <= 3; day++ {
		decision := rule.Evaluate(analysisInput(ruleBase.AddDate(0, 0, day), .64), state, false, config)
		state = decision.Next
		if day == 3 && !decision.ShouldOpen {
			t.Fatal("third low day did not open")
		}
	}
	firstRecovery := rule.Evaluate(analysisInput(ruleBase.AddDate(0, 0, 4), .80), state, true, config)
	if firstRecovery.ShouldResolve || firstRecovery.Next.Consecutive != 1 {
		t.Fatalf("first recovery resolved an alert opened with a three-day streak: %+v", firstRecovery)
	}
	secondRecovery := rule.Evaluate(analysisInput(ruleBase.AddDate(0, 0, 5), .80), firstRecovery.Next, true, config)
	if !secondRecovery.ShouldResolve {
		t.Fatalf("second recovery did not resolve: %+v", secondRecovery)
	}
}

func sunnyInput(at time.Time, elevation, irradiance, coverage float64) Input {
	return Input{At: at, TelemetryObserved: true, TelemetryFresh: true, ACPowerW: 0, SolarElevationDeg: elevation, IrradianceWM2: irradiance, WeatherAvailable: true, TelemetryCoveragePct: coverage, GridVoltageV: 220, GridFrequencyHz: 60}
}

func telemetryInput(at time.Time, voltage, frequency float64) Input {
	return Input{At: at, TelemetryObserved: true, TelemetryFresh: true, GridVoltageV: voltage, GridFrequencyHz: frequency}
}

func analysisInput(at time.Time, ratio float64) Input {
	return Input{At: at, WeatherAvailable: true, AnalysisDay: at.Format("2006-01-02"), Analysis: &domain.AnalysisResult{ExpectedWh: 100, ActualWh: ratio * 100, Ratio: ratio, Confidence: domain.ConfidenceHigh, Qualifying: true}}
}
