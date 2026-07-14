package collector

import (
	"sync"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

type State struct {
	Snapshot    *domain.TelemetrySnapshot `json:"snapshot,omitempty"`
	LastSuccess time.Time                 `json:"lastSuccess,omitempty"`
	LastError   string                    `json:"lastError,omitempty"`
	Stale       bool                      `json:"stale"`
}

type Event struct {
	Kind     string                    `json:"kind"`
	Snapshot *domain.TelemetrySnapshot `json:"snapshot,omitempty"`
	State    State                     `json:"state"`
}

// Hub fans events out without allowing a slow subscriber to block a publisher.
type Hub struct {
	mu          sync.Mutex
	subscribers map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan Event]struct{})}
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 1)
	h.mu.Lock()
	if h.subscribers == nil {
		h.subscribers = make(map[chan Event]struct{})
	}
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, ch)
			close(ch)
			h.mu.Unlock()
		})
	}
}

func (h *Hub) Publish(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for subscriber := range h.subscribers {
		latest := cloneEvent(event)
		select {
		case subscriber <- latest:
		default:
			select {
			case <-subscriber:
			default:
			}
			subscriber <- latest
		}
	}
}

func cloneEvent(event Event) Event {
	event.Snapshot = cloneSnapshot(event.Snapshot)
	event.State = cloneState(event.State)
	return event
}

func cloneState(state State) State {
	state.Snapshot = cloneSnapshot(state.Snapshot)
	return state
}

func cloneSnapshot(snapshot *domain.TelemetrySnapshot) *domain.TelemetrySnapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.FaultCodes = append([]uint16(nil), snapshot.FaultCodes...)
	return &copy
}
