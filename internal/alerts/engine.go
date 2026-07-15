package alerts

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Repository interface {
	Transact(context.Context, func(Current) (Mutation, error)) ([]Transition, error)
}

type Engine struct {
	repository Repository
	config     Config
	rules      []Rule
}

func NewEngine(repository Repository, config Config) (*Engine, error) {
	if repository == nil {
		return nil, errors.New("alert engine: repository is required")
	}
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("alert engine config: %w", err)
	}
	return &Engine{repository: repository, config: config, rules: []Rule{
		LoggerOfflineRule(), TelemetryStaleRule(), InverterFaultRule(), ZeroSunnyGenerationRule(),
		PersistentUnderproductionRule(), GridVoltageRule(), GridFrequencyRule(),
	}}, nil
}

func (e *Engine) Evaluate(ctx context.Context, input Input) ([]Transition, error) {
	if err := validateInput(input); err != nil {
		return nil, fmt.Errorf("evaluate alerts: %w", err)
	}
	return e.repository.Transact(ctx, func(current Current) (Mutation, error) {
		states := make(map[string]State, len(e.rules))
		for key, state := range current.States {
			states[key] = state
		}
		open := current.Open
		if open == nil {
			open = map[string]bool{}
		}
		transitions := make([]Transition, 0)
		for _, rule := range e.rules {
			state := states[rule.Name()]
			if !state.LastEvaluatedAt.IsZero() && input.At.Before(state.LastEvaluatedAt) {
				return Mutation{}, fmt.Errorf("clock regressed for %s: %s before %s", rule.Name(), input.At.UTC().Format(time.RFC3339Nano), state.LastEvaluatedAt.UTC().Format(time.RFC3339Nano))
			}
			decision := rule.Evaluate(input, state, open[rule.Name()], e.config)
			states[rule.Name()] = decision.Next
			if decision.ShouldOpen && !open[rule.Name()] {
				transitions = append(transitions, Transition{Rule: rule.Name(), Kind: TransitionOpened, Severity: decision.Severity, At: input.At.UTC(), Evidence: decision.Evidence, Reason: decision.Reason})
				open[rule.Name()] = true
			} else if decision.ShouldResolve && open[rule.Name()] {
				transitions = append(transitions, Transition{Rule: rule.Name(), Kind: TransitionResolved, Severity: decision.Severity, At: input.At.UTC(), Evidence: decision.Evidence, Reason: decision.Reason})
				delete(open, rule.Name())
			}
		}
		return Mutation{States: states, Transitions: transitions}, nil
	})
}

func validateConfig(config Config) error {
	if config.StaleAfter <= 0 || config.ZeroGenerationFor <= 0 || config.VoltageFor <= 0 || config.FrequencyFor <= 0 || config.UnderproductionDays <= 0 || config.RecoveryDays <= 0 {
		return errors.New("durations and day counts must be positive")
	}
	values := []float64{config.MinimumSolarElevation, config.MinimumIrradianceWM2, config.MinimumCoveragePct, config.MaximumZeroPowerW, config.UnderproductionRatio, config.RecoveryRatio, config.MinimumVoltageV, config.MaximumVoltageV, config.MinimumFrequencyHz, config.MaximumFrequencyHz}
	for _, value := range values {
		if !finite(value) {
			return errors.New("numeric thresholds must be finite")
		}
	}
	if config.MinimumCoveragePct < 0 || config.MinimumCoveragePct > 100 || config.MaximumZeroPowerW < 0 || config.MinimumIrradianceWM2 < 0 || config.UnderproductionRatio < 0 || config.RecoveryRatio > 1 || config.UnderproductionRatio >= config.RecoveryRatio || config.MinimumVoltageV >= config.MaximumVoltageV || config.MinimumFrequencyHz >= config.MaximumFrequencyHz {
		return errors.New("threshold ranges are invalid")
	}
	return nil
}

func validateInput(input Input) error {
	if input.At.IsZero() {
		return errors.New("evaluation time is required")
	}
	if !input.LastTelemetryAt.IsZero() && input.LastTelemetryAt.After(input.At) {
		return errors.New("last telemetry time is in the future")
	}
	for _, value := range []float64{input.ACPowerW, input.SolarElevationDeg, input.IrradianceWM2, input.TelemetryCoveragePct, input.GridVoltageV, input.GridFrequencyHz} {
		if !finite(value) {
			return errors.New("numeric input must be finite")
		}
	}
	if input.TelemetryCoveragePct < 0 || input.TelemetryCoveragePct > 100 || input.IrradianceWM2 < 0 || input.ACPowerW < 0 || input.GridVoltageV < 0 || input.GridFrequencyHz < 0 {
		return errors.New("telemetry values are outside valid ranges")
	}
	if input.Analysis != nil {
		if _, err := time.Parse("2006-01-02", input.AnalysisDay); err != nil {
			return errors.New("analysis day must be YYYY-MM-DD")
		}
		if !finite(input.Analysis.ExpectedWh) || !finite(input.Analysis.ActualWh) || !finite(input.Analysis.Ratio) || input.Analysis.ExpectedWh < 0 || input.Analysis.ActualWh < 0 || input.Analysis.Ratio < 0 || !input.Analysis.Confidence.Valid() {
			return errors.New("analysis values are invalid")
		}
	}
	return nil
}
