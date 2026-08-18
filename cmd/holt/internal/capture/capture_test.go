package capture_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/cmd/holt/internal/capture"
	"github.com/openotters/holt/pkg/attach"
	"github.com/openotters/holt/pkg/jwtauth"
	"github.com/openotters/holt/pkg/peername"
	"github.com/openotters/holt/pkg/registry"
)

// hub is a real tunnel front door on loopback; endpoints attach to it
// exactly as real peers would.
type hub struct {
	registry *registry.Registry
	manager  *capture.Manager
}

func newHub(t *testing.T) *hub {
	t.Helper()

	logger := zap.NewNop()
	reg := registry.NewRegistry(logger)
	secret := jwtauth.NewSecret([]byte("test-secret-value-for-signing-only"))

	guard := jwtauth.Guard{Secret: secret}
	srv := httptest.NewServer(guard.Middleware(attach.NewHandler(reg, jwtauth.PeerFrom, logger)))
	t.Cleanup(srv.Close)

	// The httptest URL is http://; dial normalizes it to ws://.
	return &hub{
		registry: reg,
		manager:  capture.NewManager(t.Context(), srv.URL, secret, reg, logger),
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
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

func TestEndpointAttachesAndAcknowledges(t *testing.T) {
	t.Parallel()

	h := newHub(t)

	bin, err := h.manager.Create("", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := peername.Validate(bin.Peer); err != nil {
		t.Fatalf("generated name %q is not a valid peer name: %v", bin.Peer, err)
	}

	waitFor(t, "endpoint to attach", func() bool { return h.registry.Attached(bin.Peer) })

	req, err := http.NewRequestWithContext(t.Context(),
		http.MethodPost, "http://peer.invalid/hook?x=1", strings.NewReader(`{"hello":"bin"}`))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}

	resp, err := h.registry.RoundTripper(bin.Peer).RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip through the tunnel: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}

	var got struct {
		Captured bool   `json:"captured"`
		Capture  string `json:"capture"`
		Method   string `json:"method"`
		Path     string `json:"path"`
	}

	if decodeErr := json.NewDecoder(resp.Body).Decode(&got); decodeErr != nil {
		t.Fatalf("response is not JSON: %v", decodeErr)
	}

	if !got.Captured || got.Capture != bin.Peer || got.Method != http.MethodPost || got.Path != "/hook?x=1" {
		t.Fatalf("acknowledgement %+v does not describe the request", got)
	}
}

func TestStopDetachesTheEndpoint(t *testing.T) {
	t.Parallel()

	h := newHub(t)

	bin, err := h.manager.Create("", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	waitFor(t, "endpoint to attach", func() bool { return h.registry.Attached(bin.Peer) })

	if !h.manager.Stop(bin.Peer) {
		t.Fatal("Stop reported no such endpoint")
	}

	if h.manager.Stop(bin.Peer) {
		t.Fatal("second Stop reported the endpoint still there")
	}

	waitFor(t, "tunnel to detach", func() bool { return !h.registry.Attached(bin.Peer) })

	if got := h.manager.List(); len(got) != 0 {
		t.Fatalf("List after Stop = %v, want empty", got)
	}
}

func TestEndpointExpires(t *testing.T) {
	t.Parallel()

	h := newHub(t)

	// Whole seconds: JWT expiry has one-second resolution, so a
	// sub-second TTL can mint a token that is dead on arrival.
	bin, err := h.manager.Create("", 2*time.Second)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	waitFor(t, "endpoint to attach", func() bool { return h.registry.Attached(bin.Peer) })
	waitFor(t, "endpoint to expire", func() bool { return !h.registry.Attached(bin.Peer) })
	waitFor(t, "endpoint to leave the list", func() bool { return len(h.manager.List()) == 0 })
}

func TestCreateRefusesALivePeersName(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	secret := jwtauth.NewSecret([]byte("test-secret-value-for-signing-only"))
	manager := capture.NewManager(t.Context(), "ws://127.0.0.1:1", secret, attachedStub{}, logger)

	if _, err := manager.Create("alice", 0); err == nil {
		t.Fatal("Create under a live peer's name did not fail")
	}
}

func TestCreateValidatesNames(t *testing.T) {
	t.Parallel()

	h := newHub(t)

	if _, err := h.manager.Create("Not-Valid!", 0); err == nil {
		t.Fatal("invalid peer name did not fail")
	}

	if _, err := h.manager.Create("mybin", 0); err != nil {
		t.Fatalf("Create(mybin): %v", err)
	}

	if _, err := h.manager.Create("mybin", 0); err == nil {
		t.Fatal("duplicate endpoint name did not fail")
	}
}

// attachedStub reports every peer as live.
type attachedStub struct{}

func (attachedStub) Attached(string) bool           { return true }
func (attachedStub) StopTunnel(string, string) bool { return false }
