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

	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/cmd/holt/internal/peername"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/cmd/holt/internal/token"
)

// Enroll mints a join token for a peer. With an admin endpoint
// configured (--admin-url / --profile / --header) it asks a running hub
// to mint one, so the token carries the hub's own advertised URL and no
// --tunnel-url is needed. Otherwise it works locally, signing with the
// JWT secret in the state folder — offline, on the hub machine.
type Enroll struct {
	Peer string `arg:"" help:"Identity to mint the token for (DNS label: lowercase letters, digits, dashes)."`

	// Tunnel URL advertised in the token (its scheme selects the peer
	// transport). Resolves flag > env > profile tunnel_url; local mode
	// falls back to http://127.0.0.1:7000, remote mode to the hub's
	// --advertise-addr.
	TunnelURL string        `help:"Tunnel URL to advertise, e.g. https://holt.example.com (default: profile tunnel_url, then http://127.0.0.1:7000; the hub's advertised URL when remote)." name:"tunnel-url" env:"HOLT_TUNNEL_URL"`
	State     string        `help:"Hub state directory (JWT secret)." type:"path"`
	TokenTTL  time.Duration `help:"Lifetime of the minted JWT (local mode)." default:"24h"`

	// Remote mode: --admin-url / --header / --profile / --config.
	adminConn
}

// Run mints and prints a join token, locally or via a remote hub.
func (e *Enroll) Run(ctx context.Context, _ *c.Commons) error {
	// A peer id has to work as a DNS label so the hostname routing
	// strategies can reach it; refuse to mint a name that cannot.
	if err := peername.Validate(e.Peer); err != nil {
		return err
	}

	ep, err := e.endpoint()
	if err != nil {
		return err
	}

	prof, err := e.profile()
	if err != nil {
		return err
	}

	// Advertised URL, same precedence as everything else: flag/env
	// (folded by kong) then profile. Empty means "let the mode decide"
	// (loopback default local, hub's advertised URL remote).
	tunnelURL := coalesce(e.TunnelURL, prof.TunnelURL)

	if ep.remote {
		return e.enrollRemote(ctx, ep, tunnelURL)
	}

	return e.enrollLocal(tunnelURL)
}

// enrollRemote asks the hub to mint the token. Without a tunnelURL the
// hub stamps its own advertised URL; with one, it uses the override.
func (e *Enroll) enrollRemote(ctx context.Context, ep endpoint, tunnelURL string) error {
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
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ep.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("reaching hub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("hub enroll failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out struct {
		Token string `json:"token"`
	}
	if decErr := json.NewDecoder(resp.Body).Decode(&out); decErr != nil {
		return fmt.Errorf("decoding hub response: %w", decErr)
	}

	printToken(e.Peer, out.Token)

	return nil
}

// enrollLocal signs a token from the on-disk JWT secret.
func (e *Enroll) enrollLocal(tunnelURL string) error {
	if e.State == "" {
		e.State = defaultStateDir()
	}

	if tunnelURL == "" {
		tunnelURL = "http://127.0.0.1:7000"
	}

	secret, err := hubsecret.Load(e.State)
	if err != nil {
		return err
	}

	jwtStr, err := jwtauth.Issue(secret, e.Peer, e.TokenTTL)
	if err != nil {
		return err
	}

	tok := token.JoinToken{
		Peer:      e.Peer,
		TunnelURL: tunnelURL,
		JWT:       jwtStr,
	}.Encode()

	printToken(e.Peer, tok)

	return nil
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
