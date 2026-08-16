package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt"
	"github.com/openotters/holt/internal/dial"
	"github.com/openotters/holt/internal/registry"
	"github.com/openotters/holt/internal/revproxy"
)

// listen binds a throwaway loopback listener the test hands to the
// server, which is how a test gets a port without racing for one.
func listen(t *testing.T) net.Listener {
	t.Helper()

	var lc net.ListenConfig

	lis, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	return lis
}

// verifyToken is the demo credential check: two tokens, each proving
// one peer.
func verifyToken(_ context.Context, token string) (string, error) {
	peers := map[string]string{"tok-alice": "alice", "tok-bob": "bob"}
	if peer, ok := peers[token]; ok {
		return peer, nil
	}

	return "", errors.New("unknown token")
}

// The whole promise of NewServer in one test: a tunnel with bearer
// auth, a proxy, a peer that attaches with its token, a request that
// reaches it through the proxy, and a clean drain on cancel.
func TestServerEndToEnd(t *testing.T) {
	t.Parallel()

	tunnelLis, proxyLis := listen(t), listen(t)

	// The proxy takes middleware too — here one that marks every
	// response, standing in for metrics or access logging.
	marked := holt.Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Via", "hub")
			next.ServeHTTP(w, r)
		})
	})

	srv := holt.NewServer(
		holt.WithLogger(zap.NewNop()),
		holt.WithTunnel(holt.NewTunnel("",
			holt.WithListener(tunnelLis),
			holt.WithAuthBearer(verifyToken),
		)),
		holt.WithProxy(holt.NewProxy("",
			holt.WithListener(proxyLis),
			holt.WithMiddleware(marked),
		)),
	)

	ctx, cancel := context.WithCancel(t.Context())

	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	// A peer attaches with its bearer token and serves a handler only
	// it could serve.
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/whoami", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "alice through the tunnel")
	})

	go func() {
		_ = dial.Run(ctx, dial.Options{
			URL:     "ws://" + tunnelLis.Addr().String(),
			Header:  http.Header{"Authorization": {"Bearer tok-alice"}},
			Handler: peerMux,
			Version: "server-test",
			Logger:  zap.NewNop(),
		})
	}()

	waitAttached(t, srv.Registry(), "alice")

	// Reach the peer through the proxy endpoint, the way any HTTP
	// client would.
	proxyURL := "http://" + proxyLis.Addr().String()

	resp := get(t, proxyURL+"/whoami", "alice")
	if resp.status != http.StatusOK || resp.body != "alice through the tunnel" {
		t.Fatalf("through proxy: %d %q", resp.status, resp.body)
	}

	if resp.via != "hub" {
		t.Fatal("the proxy middleware did not run")
	}

	// An absent peer is a 404, not a 502, and the body says nothing.
	if resp = get(t, proxyURL+"/whoami", "nobody"); resp.status != http.StatusNotFound {
		t.Fatalf("absent peer: status %d, want 404", resp.status)
	}

	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned %v on a plain cancel", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// WithAuthBearer guards the upgrade itself: no token and a wrong
// token both answer 401 and never reach the registry.
func TestServerBearerGuardsAttach(t *testing.T) {
	t.Parallel()

	tunnelLis := listen(t)

	srv := holt.NewServer(
		holt.WithTunnel(holt.NewTunnel("",
			holt.WithListener(tunnelLis),
			holt.WithAuthBearer(verifyToken),
		)),
		holt.WithProxy(nil),
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	cases := []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"unknown token", "tok-mallory"},
	}

	for _, tc := range cases {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+tunnelLis.Addr().String()+"/", nil)
		if err != nil {
			t.Fatal(err)
		}

		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", tc.name, resp.StatusCode)
		}
	}

	cancel()
	<-runDone
}

// The generic pair — WithMiddleware stamping the context and
// WithIdentity reading it — carries any scheme WithAuthBearer does
// not cover.
func TestServerCustomMiddlewareIdentity(t *testing.T) {
	t.Parallel()

	type peerKey struct{}

	headerAuth := holt.Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := r.Header.Get("X-Peer")
			if peer == "" {
				http.Error(w, "who are you", http.StatusUnauthorized)

				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerKey{}, peer)))
		})
	})

	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerKey{}).(string)

		return peer, nil
	}

	tunnelLis := listen(t)

	srv := holt.NewServer(
		holt.WithTunnel(holt.NewTunnel("",
			holt.WithListener(tunnelLis),
			holt.WithMiddleware(headerAuth),
			holt.WithIdentity(identity),
		)),
		holt.WithProxy(nil),
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	go func() {
		_ = dial.Run(ctx, dial.Options{
			URL:     "ws://" + tunnelLis.Addr().String(),
			Header:  http.Header{"X-Peer": {"carol"}},
			Handler: http.NotFoundHandler(),
			Version: "server-test",
			Logger:  zap.NewNop(),
		})
	}()

	waitAttached(t, srv.Registry(), "carol")

	cancel()
	<-runDone
}

// Zero configuration serves: with no identity configured, the
// development identity names peers by their x-holt-peer claim, or
// generates a name for peers that claim nothing.
func TestServerDevIdentity(t *testing.T) {
	t.Parallel()

	tunnelLis := listen(t)

	srv := holt.NewServer(
		holt.WithTunnel(holt.NewTunnel("", holt.WithListener(tunnelLis))),
		holt.WithProxy(nil),
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	// One peer names itself; one claims nothing and gets a name.
	for _, header := range []http.Header{
		{holt.DevPeerHeader: {"carol"}},
		nil,
	} {
		go func() {
			_ = dial.Run(ctx, dial.Options{
				URL:     "ws://" + tunnelLis.Addr().String(),
				Header:  header,
				Handler: http.NotFoundHandler(),
				Version: "server-test",
				Logger:  zap.NewNop(),
			})
		}()
	}

	waitAttached(t, srv.Registry(), "carol")
	waitAttached(t, srv.Registry(), "peer-1")

	cancel()
	<-runDone
}

// Explicitly disabling both endpoints is the one configuration that
// cannot serve, and it fails at Run rather than idling silently.
func TestServerValidation(t *testing.T) {
	t.Parallel()

	srv := holt.NewServer(holt.WithTunnel(nil), holt.WithProxy(nil))
	if err := srv.Run(t.Context()); err == nil {
		t.Fatal("Run accepted a configuration with nothing to serve")
	}
}

// The development identity trusts what peers claim, so it is refused
// on any bind another machine could reach — before the port is even
// bound.
func TestServerDevIdentityIsLoopbackOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		addr string
	}{
		{"all interfaces", ":0"},
		{"unspecified v4", "0.0.0.0:0"},
		{"a specific non-loopback ip", "192.0.2.1:0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := holt.NewServer(
				holt.WithTunnel(holt.NewTunnel(tc.addr)),
				holt.WithProxy(nil),
			)

			if err := srv.Run(t.Context()); err == nil {
				t.Fatal("Run served the development identity on a non-loopback bind")
			}
		})
	}
}

// A taken port fails Run synchronously, and the listener bound before
// it is closed rather than leaked half-serving.
func TestServerBindFailureFailsFast(t *testing.T) {
	t.Parallel()

	taken := listen(t)
	defer func() { _ = taken.Close() }()

	srv := holt.NewServer(
		holt.WithTunnel(holt.NewTunnel("127.0.0.1:0", holt.WithAuthBearer(verifyToken))),
		holt.WithProxy(holt.NewProxy(taken.Addr().String())),
	)

	if err := srv.Run(t.Context()); err == nil {
		t.Fatal("Run bound a port that is already taken")
	}
}

// proxyResponse is what a request through the proxy came back with.
type proxyResponse struct {
	body   string
	status int
	via    string
}

func get(t *testing.T, url, peer string) proxyResponse {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set(revproxy.RouteHeader, peer)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return proxyResponse{body: string(body), status: resp.StatusCode, via: resp.Header.Get("X-Via")}
}

func waitAttached(t *testing.T, r *registry.Registry, peer string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for !r.Attached(peer) {
		if time.Now().After(deadline) {
			t.Fatalf("peer %q never attached", peer)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
