package alerts

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"
)

type memoryRepository struct {
	mu          sync.Mutex
	states      map[string]State
	open        map[string]bool
	transitions []Transition
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{states: map[string]State{}, open: map[string]bool{}}
}

func (r *memoryRepository) Transact(_ context.Context, apply func(Current) (Mutation, error)) ([]Transition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := Current{States: cloneStates(r.states), Open: cloneOpen(r.open)}
	mutation, err := apply(current)
	if err != nil {
		return nil, err
	}
	r.states = cloneStates(mutation.States)
	for _, transition := range mutation.Transitions {
		if transition.Kind == TransitionOpened && !r.open[transition.Rule] {
			r.open[transition.Rule] = true
			r.transitions = append(r.transitions, transition)
		} else if transition.Kind == TransitionResolved && r.open[transition.Rule] {
			delete(r.open, transition.Rule)
			r.transitions = append(r.transitions, transition)
		}
	}
	return append([]Transition(nil), mutation.Transitions...), nil
}

func cloneStates(in map[string]State) map[string]State {
	out := map[string]State{}
	for key, value := range in {
		out[key] = value
	}
	return out
}
func cloneOpen(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func TestAlertEngineStableOrderAndNoDuplicateOpen(t *testing.T) {
	repository := newMemoryRepository()
	engine, err := NewEngine(repository, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	in := sunnyInput(ruleBase, 11, 200, 80)
	in.PollObserved = true
	in.PollSucceeded = false
	in.LastTelemetryAt = ruleBase.Add(-time.Hour)
	in.PV1Fault = true
	in.GridVoltageV = 190
	in.GridFrequencyHz = 58
	repository.states[RuleLoggerOffline] = State{Consecutive: 2}
	repository.states[RuleZeroSunnyGeneration] = State{PendingSince: ruleBase.Add(-time.Hour)}
	repository.states[RuleGridVoltage] = State{PendingSince: ruleBase.Add(-time.Hour)}
	repository.states[RuleGridFrequency] = State{PendingSince: ruleBase.Add(-time.Hour)}

	first, err := engine.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{RuleLoggerOffline, RuleTelemetryStale, RuleInverterFault, RuleGridVoltage, RuleGridFrequency}
	if len(first) != len(want) {
		t.Fatalf("transitions=%v", first)
	}
	for index := range want {
		if first[index].Rule != want[index] {
			t.Fatalf("rule %d=%q want %q", index, first[index].Rule, want[index])
		}
	}
	second, err := engine.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("duplicate transitions=%v", second)
	}
}

func TestAlertEngineRejectsInvalidAndRegressedInputs(t *testing.T) {
	repository := newMemoryRepository()
	engine, err := NewEngine(repository, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	in := telemetryInput(ruleBase, 220, 60)
	if _, err := engine.Evaluate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.At = ruleBase.Add(-time.Nanosecond)
	if _, err := engine.Evaluate(context.Background(), in); err == nil {
		t.Fatal("clock regression accepted")
	}
	in = telemetryInput(ruleBase.Add(time.Second), 220, 60)
	in.ACPowerW = math.NaN()
	if _, err := engine.Evaluate(context.Background(), in); err == nil {
		t.Fatal("NaN accepted")
	}
}

func TestAlertEngineConcurrentEvaluationCreatesOneOpen(t *testing.T) {
	repository := newMemoryRepository()
	repository.states[RuleLoggerOffline] = State{Consecutive: 2}
	engine, err := NewEngine(repository, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	in := Input{At: ruleBase, PollObserved: true}
	var wait sync.WaitGroup
	errors := make(chan error, 20)
	wait.Add(20)
	for range 20 {
		go func() {
			defer wait.Done()
			_, err := engine.Evaluate(context.Background(), in)
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent evaluation: %v", err)
		}
	}
	count := 0
	for _, transition := range repository.transitions {
		if transition.Rule == RuleLoggerOffline && transition.Kind == TransitionOpened {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("open count=%d", count)
	}
}

func TestAlertEngineDuplicateTimestampDoesNotAdvancePollEvidence(t *testing.T) {
	repository := newMemoryRepository()
	engine, err := NewEngine(repository, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	in := Input{At: ruleBase, PollObserved: true}
	for range 20 {
		if _, err := engine.Evaluate(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	state := repository.states[RuleLoggerOffline]
	if state.Consecutive != 1 || repository.open[RuleLoggerOffline] {
		t.Fatalf("duplicate poll advanced state: %+v open=%v", state, repository.open[RuleLoggerOffline])
	}
}

func TestAlertEngineDuplicateTimestampDoesNotResolveFault(t *testing.T) {
	repository := newMemoryRepository()
	repository.open[RuleInverterFault] = true
	engine, err := NewEngine(repository, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	in := Input{At: ruleBase, TelemetryObserved: true, TelemetryFresh: true}
	if _, err := engine.Evaluate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Evaluate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if !repository.open[RuleInverterFault] || repository.states[RuleInverterFault].Consecutive != 1 {
		t.Fatalf("duplicate clear resolved fault: state=%+v open=%v", repository.states[RuleInverterFault], repository.open[RuleInverterFault])
	}
}

func TestAlertEngineRejectsNegativeTelemetryValues(t *testing.T) {
	repository := newMemoryRepository()
	engine, err := NewEngine(repository, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	in := telemetryInput(ruleBase, 220, 60)
	in.ACPowerW = -1
	if _, err := engine.Evaluate(context.Background(), in); err == nil {
		t.Fatal("negative production power accepted")
	}
}
