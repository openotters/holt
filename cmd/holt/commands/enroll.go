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

	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/cmd/holt/internal/selfsigned"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/cmd/holt/internal/token"
)

// Enroll mints a join token for a peer. With an admin endpoint
// configured (--admin-url / --profile / --header) it asks a running hub
// to mint one, so the token carries the hub's own advertise address and
// no --hub-addr is needed. Otherwise it works locally, signing with the
// cert + JWT secret in the state folder — offline, on the hub machine.
type Enroll struct {
	Peer string `arg:"" help:"Identity to mint the token for."`

	// Local mode (no admin endpoint): read state and advertise this.
	HubAddr  string        `help:"Tunnel address to advertise (local mode; the hub supplies it remotely)." default:"127.0.0.1:7000"`
	State    string        `help:"Hub state directory (cert + JWT secret)." type:"path"`
	TokenTTL time.Duration `help:"Lifetime of the minted JWT (local mode)." default:"24h"`

	// Remote mode: --admin-url / --header / --profile / --config.
	adminConn
}

// Run mints and prints a join token, locally or via a remote hub.
func (e *Enroll) Run(ctx context.Context, _ *c.Commons) error {
	ep, err := e.endpoint()
	if err != nil {
		return err
	}

	if ep.remote {
		return e.enrollRemote(ctx, ep)
	}

	return e.enrollLocal()
}

// enrollRemote asks the hub to mint the token, so its advertise address
// (not a guessed --hub-addr) lands in the token.
func (e *Enroll) enrollRemote(ctx context.Context, ep endpoint) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	payload, _ := json.Marshal(map[string]string{"peer": e.Peer})

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

// enrollLocal signs a token from the on-disk hub identity.
func (e *Enroll) enrollLocal() error {
	if e.State == "" {
		e.State = defaultStateDir()
	}

	mat, err := selfsigned.Load(e.State)
	if err != nil {
		return err
	}

	jwtStr, err := jwtauth.Issue(mat.JWTSecret, e.Peer, e.TokenTTL)
	if err != nil {
		return err
	}

	tok := token.JoinToken{
		Peer:    e.Peer,
		HubAddr: e.HubAddr,
		JWT:     jwtStr,
		CAPEM:   mat.CertPEM,
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
