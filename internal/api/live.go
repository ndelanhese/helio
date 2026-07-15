package api

import (
	"net/http"

	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
)

func (a *API) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, utcState(a.dependencies.Latest()))
}

func utcState(state collector.State) collector.State {
	state.LastSuccess = state.LastSuccess.UTC()
	state.Snapshot = utcSnapshot(state.Snapshot)
	return state
}

func utcSnapshot(snapshot *domain.TelemetrySnapshot) *domain.TelemetrySnapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.ObservedAt = copy.ObservedAt.UTC()
	// JSON clients require an array even when inverter reports no active faults.
	copy.FaultCodes = append([]uint16{}, snapshot.FaultCodes...)
	return &copy
}

func utcEvent(event collector.Event) collector.Event {
	event.State = utcState(event.State)
	event.Snapshot = utcSnapshot(event.Snapshot)
	return event
}
