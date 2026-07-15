package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	stream, unsubscribe := a.dependencies.Hub.Subscribe()
	defer unsubscribe()
	if _, err := fmt.Fprint(w, "retry: 5000\n\n"); err != nil {
		return
	}
	if !writeSSE(w, "state", utcState(a.dependencies.Latest())) {
		return
	}
	flusher.Flush()
	heartbeat := timeNewTicker(a.dependencies.SSEHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, open := <-stream:
			if !open {
				return
			}
			if !writeSSE(w, event.Kind, utcEvent(event)) {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, value any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
	return err == nil
}
