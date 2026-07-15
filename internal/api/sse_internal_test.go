package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/collector"
)

type deadlineWriter struct {
	header      http.Header
	deadlineSet bool
}

func (w *deadlineWriter) Header() http.Header { return w.header }
func (*deadlineWriter) WriteHeader(int)       {}
func (w *deadlineWriter) Write([]byte) (int, error) {
	if !w.deadlineSet {
		return 0, errors.New("write attempted without deadline")
	}
	return 0, context.DeadlineExceeded
}
func (*deadlineWriter) Flush()                             {}
func (w *deadlineWriter) SetWriteDeadline(time.Time) error { w.deadlineSet = true; return nil }

func TestSSESetsDeadlineBeforePotentiallyBlockingWrite(t *testing.T) {
	w := &deadlineWriter{header: make(http.Header)}
	a := &API{dependencies: Dependencies{Hub: collector.NewHub(), Latest: func() collector.State { return collector.State{} }, ShutdownContext: context.Background(), SSEHeartbeat: time.Hour}}
	a.events(w, httptest.NewRequest(http.MethodGet, "/api/v1/live/events", nil))
	if !w.deadlineSet {
		t.Fatal("SSE write deadline was not set")
	}
}
