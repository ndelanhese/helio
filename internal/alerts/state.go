package alerts

import "time"

const (
	RuleLoggerOffline             = "logger_offline"
	RuleTelemetryStale            = "telemetry_stale"
	RuleInverterFault             = "inverter_fault"
	RuleZeroSunnyGeneration       = "zero_sunny_generation"
	RulePersistentUnderproduction = "persistent_underproduction"
	RuleGridVoltage               = "grid_voltage"
	RuleGridFrequency             = "grid_frequency"
)

var stableRuleOrder = []string{
	RuleLoggerOffline,
	RuleTelemetryStale,
	RuleInverterFault,
	RuleZeroSunnyGeneration,
	RulePersistentUnderproduction,
	RuleGridVoltage,
	RuleGridFrequency,
}

func StableRuleOrder() []string { return append([]string(nil), stableRuleOrder...) }

type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Evidence struct {
	Values     map[string]float64   `json:"values,omitempty"`
	Timestamps map[string]time.Time `json:"timestamps,omitempty"`
}

type State struct {
	PendingSince    time.Time `json:"pendingSince,omitempty"`
	LastEvidenceAt  time.Time `json:"lastEvidenceAt,omitempty"`
	Consecutive     int       `json:"consecutive,omitempty"`
	LastKey         string    `json:"lastKey,omitempty"`
	LastEvaluatedAt time.Time `json:"lastEvaluatedAt,omitempty"`
}

type Decision struct {
	Rule          string
	ShouldOpen    bool
	ShouldResolve bool
	Severity      Severity
	Evidence      Evidence
	Reason        string
	Next          State
}

type TransitionKind string

const (
	TransitionOpened   TransitionKind = "opened"
	TransitionResolved TransitionKind = "resolved"
)

type Transition struct {
	Rule     string         `json:"rule"`
	Kind     TransitionKind `json:"kind"`
	Severity Severity       `json:"severity"`
	At       time.Time      `json:"at"`
	Evidence Evidence       `json:"evidence"`
	Reason   string         `json:"reason"`
}

type Current struct {
	States map[string]State
	Open   map[string]bool
}

type Mutation struct {
	States      map[string]State
	Transitions []Transition
}
