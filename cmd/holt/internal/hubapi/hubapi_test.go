package hubapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/cmd/holt/internal/hubapi"
	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/pkg/jwtauth"
	"github.com/openotters/holt/pkg/token"
)

const advertised = "ws://hub.example.com:7200"

func post(t *testing.T, mux *http.ServeMux, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

// The minted token is the join token `holt expose --token` takes: it
// has to decode, name the peer, and point at the hub.
func TestEnrollMintsAUsableToken(t *testing.T) {
	t.Parallel()

	secret := jwtauth.NewSecret([]byte("test-secret-value-for-signing-only"))

	mux := http.NewServeMux()
	hubapi.Enroll{Secret: secret, TunnelURL: advertised, TTL: time.Hour}.Mount(mux)

	rec := post(t, mux, "/api/enroll", `{"peer":"alice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	var got struct {
		Token   string `json:"token"`
		Command string `json:"command"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}

	peer, err := jwtauth.Verify(secret.Get(), got.Token)
	if err != nil {
		t.Fatalf("minted token does not verify: %v", err)
	}

	if peer != "alice" {
		t.Fatalf("token subject %q, want alice", peer)
	}

	jt, err := token.Decode(got.Token)
	if err != nil {
		t.Fatalf("minted token does not decode as a join token: %v", err)
	}

	if jt.TunnelURL != advertised {
		t.Fatalf("token tunnel URL %q, want the hub's advertised %q", jt.TunnelURL, advertised)
	}

	if !strings.Contains(got.Command, got.Token) {
		t.Fatalf("command %q should carry the token", got.Command)
	}
}

// A caller can point the token at a different tunnel URL, which is how
// an operator enrolls for an edge the hub does not know about.
func TestEnrollHonorsCallerTunnelURL(t *testing.T) {
	t.Parallel()

	const override = "wss://edge.example.com"

	secret := jwtauth.NewSecret([]byte("test-secret-value-for-signing-only"))

	mux := http.NewServeMux()
	hubapi.Enroll{Secret: secret, TunnelURL: advertised, TTL: time.Hour}.Mount(mux)

	rec := post(t, mux, "/api/enroll", `{"peer":"alice","tunnel_url":"`+override+`"}`)

	var got struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	jt, err := token.Decode(got.Token)
	if err != nil {
		t.Fatal(err)
	}

	if jt.TunnelURL != override {
		t.Fatalf("token tunnel URL %q, want the caller's %q", jt.TunnelURL, override)
	}
}

// The peer id doubles as a DNS label under subdomain routing, so an
// unroutable name has to be refused at mint time rather than at attach.
func TestEnrollRefusesBadRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"not json", "{"},
		{"no peer", `{}`},
		{"uppercase peer", `{"peer":"Alice"}`},
		{"underscore peer", `{"peer":"my_service"}`},
		{"dotted peer", `{"peer":"a.b"}`},
	}

	secret := jwtauth.NewSecret([]byte("test-secret-value-for-signing-only"))

	mux := http.NewServeMux()
	hubapi.Enroll{Secret: secret, TunnelURL: advertised, TTL: time.Hour}.Mount(mux)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if rec := post(t, mux, "/api/enroll", tc.body); rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want %d (body %q)", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

// fakeTunnels counts what rotate-secret closes.
type fakeTunnels struct {
	count   int
	reasons []string
}

func (f *fakeTunnels) CountTunnels() int { return f.count }

func (f *fakeTunnels) StopAllTunnels(reason string) {
	f.reasons = append(f.reasons, reason)
	f.count = 0
}

// Rotating from the console has to do all three things at once: new
// secret on disk, hot-swapped in memory, live tunnels closed. Half a
// rotation would leave tokens the operator believes are dead.
func TestConsoleRotateSecret(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	original, err := hubsecret.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}

	secret := jwtauth.NewSecret(original)
	tunnels := &fakeTunnels{count: 3}

	mux := http.NewServeMux()
	hubapi.Console{
		State:    dir,
		Secret:   secret,
		Tunnels:  tunnels,
		Settings: hubapi.Config{},
		Logger:   zap.NewNop(),
	}.Mount(mux)

	rec := post(t, mux, "/api/rotate-secret", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	var got struct {
		Rotated       bool `json:"rotated"`
		ClosedTunnels int  `json:"closedTunnels"`
	}

	if err = json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if !got.Rotated || got.ClosedTunnels != 3 {
		t.Fatalf("response %+v, want rotated with 3 closed tunnels", got)
	}

	if string(secret.Get()) == string(original) {
		t.Fatal("the live secret was not swapped, so issued tokens stay valid")
	}

	onDisk, err := hubsecret.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if string(onDisk) != string(secret.Get()) {
		t.Fatal("the secret on disk and the live one disagree; a restart would undo the rotation")
	}

	if len(tunnels.reasons) != 1 || tunnels.reasons[0] != "token-revoked" {
		t.Fatalf("tunnels closed with %v, want one token-revoked", tunnels.reasons)
	}
}

// The console reads these keys at startup; renaming one silently blanks
// a card in the UI.
func TestConsoleConfigKeys(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	hubapi.Console{
		Secret: jwtauth.NewSecret([]byte("unused")),
		Logger: zap.NewNop(),
		Settings: hubapi.Config{
			RouteHeader:  "x-tunnel-peer",
			ProxyPort:    "7202",
			ExternalURL:  "https://peers.example.com",
			TunnelURL:    advertised,
			MetricsPort:  "7203",
			ProxyRouting: "both",
			ProxyDomain:  "peers.example.com",
		},
	}.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not a flat JSON object: %v", err)
	}

	want := map[string]string{
		"routeHeader":  "x-tunnel-peer",
		"proxyPort":    "7202",
		"externalURL":  "https://peers.example.com",
		"tunnelURL":    advertised,
		"metricsPort":  "7203",
		"proxyRouting": "both",
		"proxyDomain":  "peers.example.com",
	}

	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("config[%q] = %q, want %q", key, got[key], wantValue)
		}
	}

	if len(got) != len(want) {
		t.Errorf("config has %d keys, want %d: %v", len(got), len(want), got)
	}
}
