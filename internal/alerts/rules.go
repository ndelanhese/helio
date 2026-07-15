package alerts

import (
	"math"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type Config struct {
	StaleAfter            time.Duration
	ZeroGenerationFor     time.Duration
	MinimumSolarElevation float64
	MinimumIrradianceWM2  float64
	MinimumCoveragePct    float64
	MaximumZeroPowerW     float64
	UnderproductionRatio  float64
	RecoveryRatio         float64
	UnderproductionDays   int
	RecoveryDays          int
	MinimumVoltageV       float64
	MaximumVoltageV       float64
	VoltageFor            time.Duration
	MinimumFrequencyHz    float64
	MaximumFrequencyHz    float64
	FrequencyFor          time.Duration
}

func DefaultConfig() Config {
	return Config{
		StaleAfter: 30 * time.Second, ZeroGenerationFor: 20 * time.Minute,
		MinimumSolarElevation: 10, MinimumIrradianceWM2: 200, MinimumCoveragePct: 80,
		MaximumZeroPowerW: 0, UnderproductionRatio: .65, RecoveryRatio: .80,
		UnderproductionDays: 3, RecoveryDays: 2,
		MinimumVoltageV: 202, MaximumVoltageV: 240, VoltageFor: 5 * time.Minute,
		MinimumFrequencyHz: 59.5, MaximumFrequencyHz: 60.5, FrequencyFor: 2 * time.Minute,
	}
}

type Input struct {
	At                   time.Time
	PollObserved         bool
	PollSucceeded        bool
	TelemetryObserved    bool
	TelemetryFresh       bool
	LastTelemetryAt      time.Time
	ACPowerW             float64
	SolarElevationDeg    float64
	IrradianceWM2        float64
	WeatherAvailable     bool
	TelemetryCoveragePct float64
	PV1Fault             bool
	PV2Fault             bool
	PV2Active            bool
	GridVoltageV         float64
	GridFrequencyHz      float64
	AnalysisDay          string
	Analysis             *domain.AnalysisResult
}

type Rule interface {
	Name() string
	Evaluate(Input, State, bool, Config) Decision
}

type rule struct {
	name string
	eval func(Input, State, bool, Config) Decision
}

func (r rule) Name() string { return r.name }
func (r rule) Evaluate(input Input, state State, open bool, config Config) Decision {
	decision := r.eval(input, state, open, config)
	decision.Rule = r.name
	if decision.Next.LastEvaluatedAt.IsZero() || !input.At.Before(decision.Next.LastEvaluatedAt) {
		decision.Next.LastEvaluatedAt = input.At
	}
	return decision
}

func LoggerOfflineRule() Rule       { return rule{RuleLoggerOffline, evaluateLoggerOffline} }
func TelemetryStaleRule() Rule      { return rule{RuleTelemetryStale, evaluateTelemetryStale} }
func InverterFaultRule() Rule       { return rule{RuleInverterFault, evaluateInverterFault} }
func ZeroSunnyGenerationRule() Rule { return rule{RuleZeroSunnyGeneration, evaluateZeroGeneration} }
func PersistentUnderproductionRule() Rule {
	return rule{RulePersistentUnderproduction, evaluateUnderproduction}
}
func GridVoltageRule() Rule   { return rule{RuleGridVoltage, evaluateGridVoltage} }
func GridFrequencyRule() Rule { return rule{RuleGridFrequency, evaluateGridFrequency} }

func evaluateLoggerOffline(input Input, state State, open bool, _ Config) Decision {
	d := baseDecision(state, SeverityCritical, "three consecutive logger polls failed")
	if !input.PollObserved {
		return d
	}
	if input.At.Equal(state.LastEvidenceAt) {
		return d
	}
	d.Next.LastEvidenceAt = input.At
	if input.PollSucceeded {
		d.Next.Consecutive = 0
		d.Next.PendingSince = time.Time{}
		d.ShouldResolve = open
		d.Evidence = Evidence{Values: map[string]float64{"failed_polls": 0}, Timestamps: map[string]time.Time{"observed_at": input.At.UTC()}}
		return d
	}
	if state.Consecutive == 0 {
		d.Next.PendingSince = input.At
	}
	d.Next.Consecutive++
	d.ShouldOpen = !open && d.Next.Consecutive >= 3
	d.Evidence = evidence(map[string]float64{"failed_polls": float64(d.Next.Consecutive)}, d.Next.PendingSince)
	return d
}

func evaluateTelemetryStale(input Input, state State, open bool, config Config) Decision {
	d := baseDecision(state, SeverityWarning, "telemetry exceeded the configured freshness threshold")
	if input.LastTelemetryAt.IsZero() {
		return d
	}
	age := input.At.Sub(input.LastTelemetryAt)
	stale := age >= config.StaleAfter
	d.ShouldOpen = !open && stale
	d.ShouldResolve = open && !stale
	d.Evidence = Evidence{Values: map[string]float64{"age_seconds": age.Seconds(), "threshold_seconds": config.StaleAfter.Seconds()}, Timestamps: map[string]time.Time{"last_telemetry_at": input.LastTelemetryAt.UTC()}}
	return d
}

func evaluateInverterFault(input Input, state State, open bool, _ Config) Decision {
	d := baseDecision(state, SeverityCritical, "an active inverter reported a fault")
	if !input.TelemetryObserved || !input.TelemetryFresh {
		return d
	}
	if input.At.Equal(state.LastEvidenceAt) {
		return d
	}
	d.Next.LastEvidenceAt = input.At
	activeFaults := 0
	if input.PV1Fault {
		activeFaults++
	}
	if input.PV2Active && input.PV2Fault {
		activeFaults++
	}
	d.Evidence = Evidence{Values: map[string]float64{"active_fault_sources": float64(activeFaults)}, Timestamps: map[string]time.Time{"observed_at": input.At.UTC()}}
	if activeFaults > 0 {
		d.Next.Consecutive = 0
		d.ShouldOpen = !open
		return d
	}
	if !open {
		d.Next.Consecutive = 0
		return d
	}
	d.Next.Consecutive++
	d.ShouldResolve = d.Next.Consecutive >= 2
	return d
}

func evaluateZeroGeneration(input Input, state State, open bool, config Config) Decision {
	d := baseDecision(state, SeverityWarning, "generation remained zero during verified sunny conditions")
	fault := input.PV1Fault || (input.PV2Active && input.PV2Fault)
	qualifies := input.TelemetryObserved && input.TelemetryFresh && input.WeatherAvailable && !fault &&
		input.SolarElevationDeg > config.MinimumSolarElevation && input.IrradianceWM2 >= config.MinimumIrradianceWM2 &&
		input.TelemetryCoveragePct >= config.MinimumCoveragePct && input.ACPowerW <= config.MaximumZeroPowerW
	if !qualifies {
		d.Next.PendingSince = time.Time{}
		d.ShouldResolve = open && input.TelemetryObserved && input.TelemetryFresh && input.ACPowerW > config.MaximumZeroPowerW
		if d.ShouldResolve {
			d.Evidence = Evidence{Values: map[string]float64{"power_w": input.ACPowerW}, Timestamps: map[string]time.Time{"observed_at": input.At.UTC()}}
		}
		return d
	}
	if d.Next.PendingSince.IsZero() {
		d.Next.PendingSince = input.At
	}
	d.ShouldOpen = !open && input.At.Sub(d.Next.PendingSince) >= config.ZeroGenerationFor
	d.Evidence = Evidence{Values: map[string]float64{
		"power_w": input.ACPowerW, "elevation_degrees": input.SolarElevationDeg,
		"irradiance_wm2": input.IrradianceWM2, "coverage_pct": input.TelemetryCoveragePct,
		"window_seconds": input.At.Sub(d.Next.PendingSince).Seconds(),
	}, Timestamps: map[string]time.Time{"pending_since": d.Next.PendingSince.UTC(), "observed_at": input.At.UTC()}}
	return d
}

func evaluateUnderproduction(input Input, state State, open bool, config Config) Decision {
	d := baseDecision(state, SeverityWarning, "production remained below the learned expectation")
	if input.Analysis == nil {
		return d
	}
	if state.LastKey == input.AnalysisDay {
		return d
	}
	if !input.WeatherAvailable || !input.Analysis.Qualifying {
		d.Next.Consecutive = 0
		d.Next.LastKey = input.AnalysisDay
		return d
	}
	if state.LastKey != "" && !consecutiveDay(state.LastKey, input.AnalysisDay) {
		d.Next.Consecutive = 0
	}
	d.Next.LastKey = input.AnalysisDay
	ratio := input.Analysis.Ratio
	if open {
		if ratio >= config.RecoveryRatio {
			d.Next.Consecutive++
		} else {
			d.Next.Consecutive = 0
		}
		d.ShouldResolve = d.Next.Consecutive >= config.RecoveryDays
	} else {
		if ratio < config.UnderproductionRatio {
			d.Next.Consecutive++
		} else {
			d.Next.Consecutive = 0
		}
		d.ShouldOpen = d.Next.Consecutive >= config.UnderproductionDays
	}
	qualifyingDays := d.Next.Consecutive
	d.Evidence = Evidence{Values: map[string]float64{"ratio": ratio, "qualifying_days": float64(qualifyingDays), "expected_wh": input.Analysis.ExpectedWh, "actual_wh": input.Analysis.ActualWh}, Timestamps: map[string]time.Time{"analyzed_at": input.At.UTC()}}
	if d.ShouldOpen || d.ShouldResolve {
		d.Next.Consecutive = 0
	}
	return d
}

func evaluateGridVoltage(input Input, state State, open bool, config Config) Decision {
	outside := input.GridVoltageV < config.MinimumVoltageV || input.GridVoltageV > config.MaximumVoltageV
	return evaluateDuration(input, state, open, outside, config.VoltageFor, SeverityWarning, "grid voltage remained outside configured limits", "voltage_v", input.GridVoltageV)
}

func evaluateGridFrequency(input Input, state State, open bool, config Config) Decision {
	outside := input.GridFrequencyHz < config.MinimumFrequencyHz || input.GridFrequencyHz > config.MaximumFrequencyHz
	return evaluateDuration(input, state, open, outside, config.FrequencyFor, SeverityWarning, "grid frequency remained outside configured limits", "frequency_hz", input.GridFrequencyHz)
}

func evaluateDuration(input Input, state State, open, outside bool, duration time.Duration, severity Severity, reason, key string, value float64) Decision {
	d := baseDecision(state, severity, reason)
	if !input.TelemetryObserved || !input.TelemetryFresh {
		return d
	}
	if !outside {
		d.Next.PendingSince = time.Time{}
		d.ShouldResolve = open
		if d.ShouldResolve {
			d.Evidence = Evidence{Values: map[string]float64{key: value, "window_seconds": 0}, Timestamps: map[string]time.Time{"observed_at": input.At.UTC()}}
		}
		return d
	}
	if d.Next.PendingSince.IsZero() {
		d.Next.PendingSince = input.At
	}
	d.ShouldOpen = !open && input.At.Sub(d.Next.PendingSince) >= duration
	d.Evidence = Evidence{Values: map[string]float64{key: value, "window_seconds": input.At.Sub(d.Next.PendingSince).Seconds()}, Timestamps: map[string]time.Time{"pending_since": d.Next.PendingSince.UTC(), "observed_at": input.At.UTC()}}
	return d
}

func baseDecision(state State, severity Severity, reason string) Decision {
	return Decision{Next: state, Severity: severity, Reason: reason}
}

func evidence(values map[string]float64, since time.Time) Evidence {
	result := Evidence{Values: values}
	if !since.IsZero() {
		result.Timestamps = map[string]time.Time{"pending_since": since.UTC()}
	}
	return result
}

func consecutiveDay(previous, current string) bool {
	before, err1 := time.Parse("2006-01-02", previous)
	after, err2 := time.Parse("2006-01-02", current)
	return err1 == nil && err2 == nil && before.AddDate(0, 0, 1).Equal(after)
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
