package api

import (
	"net/http"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

// handleIngestWS upgrades an incoming runner connection to a
// WebSocket. Auth has already happened via requireIngestToken on the
// /ingest subrouter. The run_id is provided as a query param; we
// don't reject unknown ids here — the wsHub creates run entries
// lazily and the stalewatcher cleans up idle ones.
func (s *server) handleIngestWS(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run_id")
	if runID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"run_id query param is required"})

		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// We already authenticate via bearer token, so CORS-style
		// origin checks are not applicable. The runner is not a
		// browser so the usual origin-header protections don't help.
		InsecureSkipVerify: true,
		// permessage-deflate is opt-in on the client side; leaving
		// CompressionMode defaulted means the server negotiates it
		// transparently when the client offers it.
	})
	if err != nil {
		s.log.WithError(err).WithField("run_id", runID).
			Warn("Failed to accept runner WS")

		return
	}

	// RegisterRunner runs the blocking read loop until the WS closes.
	s.wsHub.RegisterRunner(r.Context(), runID, ws)
}

// handleLiveRunLogsWS upgrades an incoming UI connection to a
// WebSocket. Auth is gated earlier in the router (requireAuth when
// AnonymousRead is off). run_id comes from the URL path.
func (s *server) handleLiveRunLogsWS(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	if runID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"run_id is required"})

		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Reflect-the-origin is handled by the CORS middleware
		// upstream; nhooyr's origin check would reject cross-origin
		// connections with 403. We mirror the HTTP policy (dev mode
		// allows any, explicit origins are allow-listed) by skipping
		// the library's built-in check and trusting the CORS layer.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.log.WithError(err).WithField("run_id", runID).
			Warn("Failed to accept UI WS")

		return
	}

	s.wsHub.RegisterUI(r.Context(), runID, ws)
}
