package commands

import (
	"context"
	"fmt"
	"time"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/cmd/holt/internal/selfsigned"
	"github.com/openotters/holt/cmd/holt/internal/token"
)

// Enroll mints a join token for a peer, signing it with the hub's
// stored identity. It reads the cert + JWT secret from the state
// folder directly, so it works offline on the hub machine — no running
// hub required.
type Enroll struct {
	Peer     string        `arg:"" help:"Identity to mint the token for."`
	HubAddr  string        `help:"Hub tunnel address to advertise in the token." default:"127.0.0.1:7000"`
	State    string        `help:"Hub state directory (cert + JWT secret)." type:"path"`
	TokenTTL time.Duration `help:"Lifetime of the minted JWT." default:"24h"`
}

// Run mints and prints the join command.
func (e *Enroll) Run(_ context.Context, _ *c.Commons) error {
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

	// The peer side is a separate program — the starter-client, or your
	// own (see cmd/starter-client). Any of them consumes this token.
	fmt.Printf("\nJoin token for %q — run your peer with it, e.g.:\n\n  starter-client --token %s\n\n", e.Peer, tok)

	return nil
}
