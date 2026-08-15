package hubapi

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/openotters/holt"
	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/cmd/holt/internal/webui"
)

// Config is what GET /api/config reports to the console: the addresses
// and routing settings the UI needs to show working commands. The JSON
// names are the console's contract — ui/src/App.tsx reads exactly these
// keys.
type Config struct {
	RouteHeader  string `json:"routeHeader"`
	ProxyPort    string `json:"proxyPort"`
	ExternalURL  string `json:"externalURL"`
	TunnelURL    string `json:"tunnelURL"`
	MetricsPort  string `json:"metricsPort"`
	ProxyRouting string `json:"proxyRouting"`
	ProxyDomain  string `json:"proxyDomain"`
}

// Tunnels is the live-tunnel side rotate-secret closes: *hub.Registry
// satisfies it.
type Tunnels interface {
	// CountTunnels reports how many peers are attached right now.
	CountTunnels() int
	// StopAllTunnels closes every tunnel with the given GoAway reason.
	StopAllTunnels(reason string)
}

// Console serves the web console: its static build, the settings it
// reads at startup, and the danger-zone rotate endpoint. State, Secret,
// Tunnels, and Logger are required — rotating without any one of them
// would leave tokens alive that the operator was told are dead.
type Console struct {
	// State is the hub state directory holding the signing secret.
	State string
	// Secret is the live signing secret, hot-swapped by rotate.
	Secret *jwtauth.Secret
	// Tunnels are closed when the secret rotates.
	Tunnels Tunnels
	// Settings is the payload GET /api/config answers with.
	Settings Config
	// Path serves the console from this directory instead of the
	// embedded build. Empty uses the embedded one.
	Path string
	// Logger records the rotate, which is worth a line in the log.
	Logger *zap.Logger
}

// Mount registers the console endpoints and the static build on mux.
// The static handler takes "/", so mount it after any other route.
func (c Console) Mount(mux *http.ServeMux) {
	c.mountRotate(mux)

	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, c.Settings)
	})

	mux.Handle("/", webui.Handler(c.Path))
}

// mountRotate registers the danger zone: regenerate the JWT signing
// secret on disk and hot-swap the live one, so it takes effect
// immediately. Every JWT already issued was signed with the old secret
// and stops verifying, and live tunnels are closed; peers must be
// re-enrolled.
func (c Console) mountRotate(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/rotate-secret", func(w http.ResponseWriter, _ *http.Request) {
		secret, err := hubsecret.Rotate(c.State)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		c.Secret.Set(secret)

		closed := c.Tunnels.CountTunnels()
		c.Tunnels.StopAllTunnels(holt.ReasonTokenRevoked)

		c.Logger.Warn("hub signing secret rotated via console; tokens invalidated, tunnels closed",
			zap.Int("closed_tunnels", closed))

		writeJSON(w, map[string]any{"rotated": true, "closedTunnels": closed})
	})
}
