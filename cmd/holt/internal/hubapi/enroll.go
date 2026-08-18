// Package hubapi is the hub's own HTTP surface on the admin listener,
// alongside the Admin gRPC service: the enroll endpoint that mints join
// tokens, and — with --ui — the web console with its config and
// rotate-secret endpoints.
//
// Each endpoint group is a config struct that mounts itself, so the
// command reads as what it exposes:
//
//	hubapi.Enroll{Secret: secret, TunnelURL: advertised, TTL: ttl}.Mount(mux)
//	hubapi.Console{...}.Mount(mux)
//
// The listener is plaintext and has no built-in auth (these endpoints
// mint tokens and rotate secrets): keep it on loopback or behind an
// authenticating proxy, and use httpsrv.HostGuard in front.
package hubapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/openotters/holt/pkg/jwtauth"
	"github.com/openotters/holt/pkg/peername"
)

// Enroll serves POST /api/enroll: mint a join token for a peer. It is
// always mounted, not gated on the console, so `holt enroll` works
// against a remote hub — the hub supplies its own advertise address.
type Enroll struct {
	// Secret signs the minted token. Read per request, so a rotate
	// takes effect immediately.
	Secret *jwtauth.Secret
	// TunnelURL is the URL stamped into tokens when the caller names
	// none: what peers dial to reach this hub.
	TunnelURL string
	// TTL is how long a minted token stays valid.
	TTL time.Duration
}

// Mount registers the enroll endpoint on mux.
func (e Enroll) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/enroll", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Peer      string `json:"peer"`
			TunnelURL string `json:"tunnel_url"`
		}

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)

			return
		}

		// The peer id doubles as a DNS label under subdomain routing,
		// so an unroutable name is refused at mint time.
		if err := peername.Validate(body.Peer); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		tunnelURL := body.TunnelURL
		if tunnelURL == "" {
			tunnelURL = e.TunnelURL
		}

		// The signed JWT is the whole join token: peer in the subject,
		// tunnel URL in the audience.
		tok, err := jwtauth.Issue(e.Secret.Get(), body.Peer, tunnelURL, e.TTL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		writeJSON(w, map[string]string{
			"token":   tok,
			"command": "holt expose localhost:PORT --token " + tok,
		})
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
