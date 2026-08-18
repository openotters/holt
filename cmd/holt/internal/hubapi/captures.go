package hubapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/openotters/holt/cmd/holt/internal/capture"
)

// CaptureManager is the endpoint lifecycle the console drives;
// *capture.Manager satisfies it.
type CaptureManager interface {
	Create(name string, ttl time.Duration) (capture.Bin, error)
	List() []capture.Bin
	Stop(name string) bool
}

// Captures serves the console's capture endpoints. Console-only, like
// the rest of the UI surface.
type Captures struct {
	Manager CaptureManager
}

// Mount registers the capture endpoints on mux.
func (c Captures) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/captures", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Peer       string `json:"peer"`
			TTLSeconds int64  `json:"ttlSeconds"`
		}

		// An empty body means generated name, default TTL.
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid request body", http.StatusBadRequest)

			return
		}

		bin, err := c.Manager.Create(body.Peer, time.Duration(body.TTLSeconds)*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		writeJSON(w, bin)
	})

	mux.HandleFunc("GET /api/captures", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"captures": c.Manager.List()})
	})

	mux.HandleFunc("DELETE /api/captures/{peer}", func(w http.ResponseWriter, r *http.Request) {
		if !c.Manager.Stop(r.PathValue("peer")) {
			http.Error(w, "no such capture endpoint", http.StatusNotFound)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
}
