package collector

import (
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

func TestHubSlowSubscriberKeepsNewestEvent(t *testing.T) {
	h := NewHub()
	events, unsubscribe := h.Subscribe()
	defer unsubscribe()

	h.Publish(Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ACPowerW: 1}})
	h.Publish(Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ACPowerW: 2}})

	select {
	case got := <-events:
		if got.Snapshot == nil || got.Snapshot.ACPowerW != 2 {
			t.Fatalf("event=%+v, want newest snapshot", got)
		}
	case <-time.After(time.Second):
		t.Fatal("publish blocked or event was not delivered")
	}
}

func TestHubBufferedSubscriberReceivesEveryEventInBurst(t *testing.T) {
	h := NewHub()
	events, unsubscribe := h.SubscribeBuffered(32)
	defer unsubscribe()

	for power := 1; power <= 20; power++ {
		h.Publish(Event{Kind: "snapshot", Snapshot: &domain.TelemetrySnapshot{ACPowerW: float64(power)}})
	}
	for power := 1; power <= 20; power++ {
		select {
		case got := <-events:
			if got.Snapshot == nil || got.Snapshot.ACPowerW != float64(power) {
				t.Fatalf("event %d=%+v", power, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d was lost", power)
		}
	}
}

func TestHubBufferedSubscriberShutdownReleasesBackpressure(t *testing.T) {
	h := NewHub()
	_, unsubscribe := h.SubscribeBuffered(1)
	h.Publish(Event{Kind: "first"})
	done := make(chan struct{})
	go func() {
		h.Publish(Event{Kind: "blocked"})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("publisher did not apply backpressure")
	case <-time.After(10 * time.Millisecond):
	}
	unsubscribe()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not release publisher")
	}
}

func TestHubUnsubscribeClosesAndRemovesSubscriber(t *testing.T) {
	h := NewHub()
	events, unsubscribe := h.Subscribe()
	unsubscribe()
	unsubscribe()

	if _, open := <-events; open {
		t.Fatal("subscriber channel remained open")
	}
	h.Publish(Event{Kind: "ignored"})
}

func TestHubZeroValueCanSubscribe(t *testing.T) {
	var h Hub
	events, unsubscribe := h.Subscribe()
	defer unsubscribe()
	h.Publish(Event{Kind: "state"})
	if got := <-events; got.Kind != "state" {
		t.Fatalf("event kind=%q", got.Kind)
	}
}

func TestHubPublishesIndependentSnapshotCopies(t *testing.T) {
	h := NewHub()
	one, cancelOne := h.Subscribe()
	defer cancelOne()
	two, cancelTwo := h.Subscribe()
	defer cancelTwo()

	snapshot := &domain.TelemetrySnapshot{FaultCodes: []uint16{1}}
	h.Publish(Event{Kind: "snapshot", Snapshot: snapshot, State: State{Snapshot: snapshot}})
	snapshot.FaultCodes[0] = 9

	first := <-one
	second := <-two
	first.Snapshot.FaultCodes[0] = 7
	if second.Snapshot.FaultCodes[0] != 1 || second.State.Snapshot.FaultCodes[0] != 1 {
		t.Fatalf("subscribers shared mutable snapshots: %+v", second)
	}
}
