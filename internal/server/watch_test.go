package server_test

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt"
)

// One option, both ends: the hub reports every request it carried
// (with the peer, since several tunnels share that view) and the peer
// reports the ones its own handler served (it knows only itself).
func TestRequestHookOnBothEnds(t *testing.T) {
	t.Parallel()

	tunnelLis, proxyLis := listen(t), listen(t)

	var (
		mu        sync.Mutex
		hubSeen   []holt.RequestEvent
		peerSeen  []holt.RequestEvent
		collectTo = func(dst *[]holt.RequestEvent) holt.RequestHook {
			return func(ev holt.RequestEvent) {
				mu.Lock()
				defer mu.Unlock()

				*dst = append(*dst, ev)
			}
		}
	)

	srv := holt.NewServer(
		holt.WithLogger(zap.NewNop()),
		holt.WithTunnel(holt.NewTunnel("",
			holt.WithListener(tunnelLis),
			holt.WithAuthBearer(verifyToken),
		)),
		holt.WithProxy(holt.NewProxy("",
			holt.WithListener(proxyLis),
			holt.WithRequestHook(collectTo(&hubSeen)),
		)),
	)

	go func() { _ = srv.Run(t.Context()) }()

	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/watched", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "served")
	})

	peer := holt.NewClient("ws://"+tunnelLis.Addr().String(), peerMux,
		holt.WithBearerToken("tok-alice"),
		holt.WithRequestHook(collectTo(&peerSeen)),
	)

	go func() { _ = peer.Run(t.Context()) }()

	waitAttached(t, srv.Registry(), "alice")

	if resp := get(t, "http://"+proxyLis.Addr().String()+"/watched", "alice"); resp.status != http.StatusOK {
		t.Fatalf("through proxy: %d %q", resp.status, resp.body)
	}

	// The peer's hook runs on its own goroutine, so give it a moment
	// rather than racing the assertion.
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(hubSeen) > 0 && len(peerSeen) > 0
	}, "both ends to report the request")

	mu.Lock()
	defer mu.Unlock()

	hub, peerEvent := hubSeen[0], peerSeen[0]

	if hub.Peer != "alice" {
		t.Errorf("hub event peer = %q, want alice", hub.Peer)
	}

	if peerEvent.Peer != "" {
		t.Errorf("peer event peer = %q, want empty", peerEvent.Peer)
	}

	for name, ev := range map[string]holt.RequestEvent{"hub": hub, "peer": peerEvent} {
		if ev.Method != http.MethodGet || ev.Path != "/watched" {
			t.Errorf("%s event = %s %s, want GET /watched", name, ev.Method, ev.Path)
		}

		if ev.Status != http.StatusOK {
			t.Errorf("%s event status = %d, want 200", name, ev.Status)
		}
	}
}

// waitFor polls until cond holds, failing the test if it never does.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}
