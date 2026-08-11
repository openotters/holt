package commands

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	c "github.com/merlindorin/go-shared/pkg/cmd"
	"go.uber.org/zap"

	holtv1connect "github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/cmd/holt/internal/selfsigned"
	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/cmd/holt/internal/token"
	"github.com/openotters/holt/cmd/holt/internal/webui"
	"github.com/openotters/holt/hub"
	"github.com/openotters/holt/hub/admin"
	"github.com/openotters/holt/hub/sqldir"
)

const routeHeader = "x-tunnel-peer"

type peerCtxKey struct{}

// Hub runs the reverse-tunnel hub with three listeners: a TLS+JWT
// tunnel endpoint peers attach to, an Admin gRPC endpoint (list, stop,
// block), and a header-routed proxy that reaches peer services.
type Hub struct {
	TunnelAddr string        `help:"TLS+JWT listener where peers attach." default:"127.0.0.1:7000"`
	AdminAddr  string        `help:"Admin gRPC listener (list/stop/block); also serves the console with --ui." default:"127.0.0.1:7001"`
	ProxyAddr  string        `help:"Header-routed proxy to reach peer services (x-tunnel-peer)." default:"127.0.0.1:7002"`
	State      string        `help:"Directory for the hub cert + JWT secret (default: ~/.holt)." type:"path"`
	UI         bool          `help:"Serve the web console (and its enroll endpoint) on the admin listener."`
	UIPath     string        `help:"Serve the console from this directory instead of the embedded build." type:"path"`
	TokenTTL   time.Duration `help:"Lifetime of JWTs minted by the console's enroll button." default:"24h"`
}

// Run starts the hub and blocks until the context is cancelled.
func (h *Hub) Run(ctx context.Context, _ *c.Commons, logger *zap.Logger, out *style.Output) error {
	logger = logger.Named("hub")

	if h.State == "" {
		h.State = defaultStateDir()
	}

	// Cert + JWT secret persist as files in the config folder…
	mat, err := selfsigned.LoadOrCreate(h.State, []string{"127.0.0.1", "localhost"})
	if err != nil {
		return err
	}

	// …everything else (blocklist, tunnel presence) in a SQLite DB
	// alongside them.
	st, err := store.Open(h.State)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	// Debug, not info: the welcome banner (or the "hub up" JSON line)
	// already tells the operator where state lives.
	logger.Debug("hub state ready", zap.String("dir", h.State))

	// Tunnel presence is projected into a SQL Directory on the same
	// DB (durable, and shareable across a fleet); stale rows from a
	// previous run are cleared on boot.
	dir := sqldir.New(st.DB(), sqldir.SQLite)
	if migErr := dir.Migrate(ctx); migErr != nil {
		return migErr
	}

	registry := hub.NewRegistry(logger, hub.WithHubID(hostname()), hub.WithDirectory(dir))
	if clearErr := registry.ClearStale(ctx); clearErr != nil {
		return clearErr
	}

	blocks, err := newBlockList(st)
	if err != nil {
		return err
	}

	tunnelSrv, err := h.serveTunnels(registry, mat, blocks, logger)
	if err != nil {
		return err
	}

	adminSrv, err := h.serveAdmin(registry, blocks, mat, logger)
	if err != nil {
		return err
	}

	proxySrv, err := h.serveProxy(registry)
	if err != nil {
		return err
	}

	fields := []zap.Field{
		zap.String("tunnel", h.TunnelAddr),
		zap.String("admin", h.AdminAddr),
		zap.String("proxy", h.ProxyAddr),
	}
	if h.UI {
		fields = append(fields, zap.String("console", "http://"+h.AdminAddr+"/"))
	}

	// The banner replaces the "hub up" log line for humans; production
	// (--log-format json) keeps the structured line instead.
	if out.Pretty {
		fmt.Print(h.welcomeBanner())
	} else {
		logger.Info("hub up", fields...)
	}

	<-ctx.Done()

	// Ctrl-C lands mid-line, hence the leading newline. Closing can
	// take a moment (peers get a GoAway, listeners drain), so say so
	// instead of looking frozen.
	if out.Pretty {
		fmt.Printf("\n%s\n", style.Note("shutting down: closing %d tunnel(s), draining listeners (up to %s grace)...",
			registry.CountTunnels(), gracePeriod))
	} else {
		logger.Info("shutting down", zap.Int("tunnels", registry.CountTunnels()))
	}

	registry.StopAllTunnels("shutting-down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
	defer cancel()

	_ = tunnelSrv.Shutdown(shutdownCtx)
	_ = adminSrv.Shutdown(shutdownCtx)
	_ = proxySrv.Shutdown(shutdownCtx)

	if out.Pretty {
		fmt.Println(style.Success("stopped cleanly"))
	}

	return nil
}

// gracePeriod bounds how long shutdown waits for tunnels and listeners
// to drain before exiting anyway.
const gracePeriod = 5 * time.Second

// welcomeBanner renders the pretty startup block with the addresses
// and a first-step hint.
func (h *Hub) welcomeBanner() string {
	rows := []style.BannerRow{
		{Key: "tunnel", Value: h.TunnelAddr, Hint: "peers attach here (TLS + JWT)"},
		{Key: "admin", Value: h.AdminAddr, Hint: "holt ls / kill / block"},
	}
	if h.UI {
		rows = append(rows,
			style.BannerRow{Key: "console", Value: "http://" + h.AdminAddr + "/", Hint: "web console"})
	}

	rows = append(rows,
		style.BannerRow{Key: "proxy", Value: h.ProxyAddr, Hint: "reach peers: curl -H 'x-tunnel-peer: <peer>'"},
		style.BannerRow{Key: "state", Value: tildePath(h.State), Hint: "cert, JWT secret, blocklist"})

	return style.Banner("holt is up", rows, "enroll your first peer:  holt enroll <name>")
}

// serveTunnels runs the TLS tunnel listener; peers authenticate with a
// JWT whose subject becomes the tunnel key. A blocked subject is
// rejected even with a valid token.
func (h *Hub) serveTunnels(
	registry *hub.Registry, mat *selfsigned.Material, blocks *blockList, logger *zap.Logger,
) (*http.Server, error) {
	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("unauthenticated")
		}

		return peer, nil
	}

	path, handler := holtv1connect.NewTunnelHandler(hub.NewHandler(registry, identity, logger))

	mux := http.NewServeMux()
	mux.Handle(path, jwtMiddleware(mat.JWTSecret, blocks, handler))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{mat.Cert}, MinVersion: tls.VersionTLS13},
	}

	lis, err := listen(h.TunnelAddr)
	if err != nil {
		return nil, err
	}

	go func() {
		if serveErr := srv.ServeTLS(lis, "", ""); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("tunnel serve", zap.Error(serveErr))
		}
	}()

	return srv, nil
}

// serveAdmin runs the Admin gRPC service (list / stop / block) and,
// with --ui, the web console plus its enroll endpoint.
func (h *Hub) serveAdmin(
	registry *hub.Registry, blocks *blockList, mat *selfsigned.Material, logger *zap.Logger,
) (*http.Server, error) {
	mux := http.NewServeMux()

	adminPath, adminHandler := holtv1connect.NewAdminHandler(
		admin.NewService(registry, admin.WithBlocker(blocks)),
	)
	mux.Handle(adminPath, adminHandler)

	if h.UI {
		// The console's "add" button mints a join token; the browser
		// posts {peer} here and gets back the token + a run command.
		mux.HandleFunc("POST /api/enroll", func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Peer string `json:"peer"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Peer == "" {
				http.Error(w, "peer is required", http.StatusBadRequest)

				return
			}

			tok, err := mintToken(mat, h.TunnelAddr, body.Peer, h.TokenTTL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)

				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"token":   tok,
				"command": "expose --token " + tok + " --target localhost:PORT",
			})
		})

		mux.Handle("/", webui.Handler(h.UIPath))

		logger.Debug("web console enabled", zap.String("url", "http://"+h.AdminAddr+"/"))
	}

	srv := newH2CServer(mux)

	lis, err := listen(h.AdminAddr)
	if err != nil {
		return nil, err
	}

	go func() {
		if serveErr := srv.Serve(lis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("admin serve", zap.Error(serveErr))
		}
	}()

	return srv, nil
}

// serveProxy runs the header-routed reverse proxy.
func (h *Hub) serveProxy(registry *hub.Registry) (*http.Server, error) {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "peer.invalid"
			pr.Out.Host = "peer.invalid"
		},
		Transport:     peerRouter{registry: registry},
		FlushInterval: -1,
		ErrorHandler:  proxyError,
	}

	srv := newH2CServer(proxy)

	lis, err := listen(h.ProxyAddr)
	if err != nil {
		return nil, err
	}

	go func() { _ = srv.Serve(lis) }()

	return srv, nil
}

// jwtMiddleware verifies the Bearer JWT, rejects blocked subjects, and
// stamps the peer id onto the request context for the identity func.
func jwtMiddleware(secret []byte, blocks *blockList, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

		peer, err := jwtauth.Verify(secret, bearer)
		if err != nil {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)

			return
		}

		if blocks.IsBlocked(peer) {
			http.Error(w, "forbidden: peer is blocked", http.StatusForbidden)

			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerCtxKey{}, peer)))
	})
}

// peerRouter dispatches each request down the tunnel named in the
// route header.
type peerRouter struct {
	registry *hub.Registry
}

func (pr peerRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	peer := req.Header.Get(routeHeader)
	if peer == "" {
		return nil, errors.New("set the " + routeHeader + " header to the target peer id")
	}

	req.Header.Del(routeHeader)

	if !pr.registry.Attached(peer) {
		return nil, fmt.Errorf("peer %q is not attached", peer)
	}

	return pr.registry.RoundTripper(peer).RoundTrip(req)
}

// proxyError renders a tunnel/proxy failure as a gRPC status (for
// grpcurl) or a readable body (for curl).
func proxyError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "14") // UNAVAILABLE
		w.Header().Set("Grpc-Message", err.Error())
		w.WriteHeader(http.StatusOK)

		return
	}

	http.Error(w, err.Error(), http.StatusBadGateway)
}

// mintToken issues a JWT for peer and packages a join token — the same
// token `holt enroll` prints, but minted server-side for the
// console's enroll button.
func mintToken(mat *selfsigned.Material, hubAddr, peer string, ttl time.Duration) (string, error) {
	jwtStr, err := jwtauth.Issue(mat.JWTSecret, peer, ttl)
	if err != nil {
		return "", err
	}

	return token.JoinToken{Peer: peer, HubAddr: hubAddr, JWT: jwtStr, CAPEM: mat.CertPEM}.Encode(), nil
}

func newH2CServer(handler http.Handler) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, Protocols: &protocols}
}

func listen(addr string) (net.Listener, error) {
	var lc net.ListenConfig

	return lc.Listen(context.Background(), "tcp", addr)
}

func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}

	return "hub"
}
