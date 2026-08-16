package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/config"
	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/pkg/jwtauth"
	"github.com/openotters/holt/pkg/peername"
)

// Enroll mints a join token for a peer. With an admin endpoint
// configured (--admin-url / --profile / --header) it asks a running hub
// to mint one, so the token carries the hub's own advertised URL and no
// --tunnel-url is needed. Otherwise it works locally, signing with the
// JWT secret in the state folder — offline, on the hub machine.
type Enroll struct {
	Peer string `arg:"" help:"Identity to mint the token for (DNS label: lowercase letters, digits, dashes)."`

	// Tunnel URL advertised in the token (its scheme selects the peer
	// transport). Resolves flag > env > profile tunnel_url, the profile
	// applying only while it still describes the hub being enrolled
	// against; local mode then falls back to ws://127.0.0.1:7000, remote
	// mode to the hub's --advertise-addr.
	TunnelURL string        `help:"Tunnel URL to advertise, e.g. https://holt.example.com (default: the profile's tunnel_url for its own hub, then the hub's advertised URL when remote, else ws://127.0.0.1:7000)." name:"tunnel-url" env:"HOLT_TUNNEL_URL"`
	State     string        `help:"Hub state directory (JWT secret)." type:"path"`
	TokenTTL  time.Duration `help:"Lifetime of the minted JWT (local mode)." default:"24h"`

	// Remote mode: --admin-url / --header / --profile / --config.
	adminConn
}

// Run mints and prints a join token, locally or via a remote hub.
func (e *Enroll) Run(ctx context.Context, _ *c.Commons) error {
	tok, err := e.mint(ctx)
	if err != nil {
		return err
	}

	printToken(e.Peer, tok)

	return nil
}

// mint produces the join token without printing it, so `holt expose`
// can enroll itself when no token was given.
func (e *Enroll) mint(ctx context.Context) (string, error) {
	// A peer id has to work as a DNS label so the hostname routing
	// strategies can reach it; refuse to mint a name that cannot.
	if err := peername.Validate(e.Peer); err != nil {
		return "", err
	}

	ep, err := e.endpoint()
	if err != nil {
		return "", err
	}

	prof, err := e.profile()
	if err != nil {
		return "", err
	}

	tunnelURL := e.advertisedURL(prof)

	if ep.remote {
		return e.enrollRemote(ctx, ep, tunnelURL)
	}

	return e.enrollLocal(tunnelURL)
}

// advertisedURL is the tunnel URL to stamp into the token: the flag or
// env if given (folded by kong), otherwise the profile's tunnel_url —
// but only while the profile still describes the hub being enrolled
// against. Pointing --admin-url / --admin-addr at another hub drops it,
// so the token advertises the hub that minted it rather than the one
// the profile happens to name. Empty means "let the mode decide": the
// hub's own advertised URL when remote, the loopback default locally.
func (e *Enroll) advertisedURL(prof config.Profile) string {
	if e.TunnelURL != "" {
		return e.TunnelURL
	}

	if e.pointsAtAnotherHub(prof) {
		return ""
	}

	return prof.TunnelURL
}

// enrollRemote asks the hub to mint the token. Without a tunnelURL the
// hub stamps its own advertised URL; with one, it uses the override.
func (e *Enroll) enrollRemote(ctx context.Context, ep endpoint, tunnelURL string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body := map[string]string{"peer": e.Peer}
	if tunnelURL != "" {
		body["tunnel_url"] = tunnelURL
	}

	payload, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		strings.TrimRight(ep.url, "/")+"/api/enroll", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ep.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("reaching hub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return "", fmt.Errorf("hub enroll failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out struct {
		Token string `json:"token"`
	}
	if decErr := json.NewDecoder(resp.Body).Decode(&out); decErr != nil {
		return "", fmt.Errorf("decoding hub response: %w", decErr)
	}

	return out.Token, nil
}

// enrollLocal signs a token from the on-disk JWT secret.
func (e *Enroll) enrollLocal(tunnelURL string) (string, error) {
	if e.State == "" {
		e.State = defaultStateDir()
	}

	if tunnelURL == "" {
		tunnelURL = "ws://127.0.0.1:7000"
	}

	secret, err := hubsecret.Load(e.State)
	if err != nil {
		return "", err
	}

	// The signed JWT is the whole token: the peer is its subject, the
	// tunnel URL its audience.
	tok, err := jwtauth.Issue(secret, e.Peer, tunnelURL, e.TokenTTL)
	if err != nil {
		return "", err
	}

	return tok, nil
}

// printToken prints the token on its own line (easy to copy or pipe)
// framed by a hint.
func printToken(peer, tok string) {
	fmt.Printf("\n%s\n\n", style.Success("join token for %q", peer))
	fmt.Printf("%s\n\n", tok)
	fmt.Println(style.Note("give it to your peer, for example:"))
	fmt.Println(style.Note("  holt expose localhost:3000 --token <paste>"))
	fmt.Println()
}
