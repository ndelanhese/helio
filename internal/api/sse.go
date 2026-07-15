package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const sseWriteTimeout = 5 * time.Second

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	_, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	stream, unsubscribe := a.dependencies.Hub.Subscribe()
	defer unsubscribe()
	if !setSSEWriteDeadline(controller) {
		return
	}
	if _, err := fmt.Fprint(w, "retry: 5000\n\n"); err != nil {
		return
	}
	if !writeSSE(w, "state", utcState(a.dependencies.Latest())) {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}
	heartbeat := timeNewTicker(a.dependencies.SSEHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.dependencies.ShutdownContext.Done():
			return
		case <-heartbeat.C:
			if !setSSEWriteDeadline(controller) {
				return
			}
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		case event, open := <-stream:
			if !open {
				return
			}
			if !setSSEWriteDeadline(controller) {
				return
			}
			if !writeSSE(w, event.Kind, utcEvent(event)) {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

func setSSEWriteDeadline(controller *http.ResponseController) bool {
	err := controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	return err == nil || errors.Is(err, http.ErrNotSupported)
}

func writeSSE(w http.ResponseWriter, event string, value any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
	return err == nil
}
