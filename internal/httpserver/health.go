package httpserver

import "net/http"

func readinessHandler(ready func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if ready != nil && ready() != nil {
			jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
