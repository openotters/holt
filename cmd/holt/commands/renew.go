package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/selfsigned"
	"github.com/openotters/holt/cmd/holt/internal/style"
)

// Renew regenerates the hub's TLS certificate (preserving its SANs and
// the JWT secret). Peers pin the current certificate through their
// enroll token, so renewing invalidates every token already handed
// out: they must be re-enrolled. Destructive, hence the confirmation.
type Renew struct {
	State string `help:"Hub state directory (default: ~/.holt)." type:"path"`
	Yes   bool   `help:"Skip the confirmation prompt (for automation)." short:"y"`
}

// Run renews the certificate after an interactive confirmation.
func (rn *Renew) Run(_ context.Context, _ *c.Commons, out *style.Output) error {
	if rn.State == "" {
		rn.State = defaultStateDir()
	}

	if !rn.Yes && !rn.confirm() {
		fmt.Println(style.Note("aborted, certificate unchanged"))

		return nil
	}

	if _, err := selfsigned.Renew(rn.State); err != nil {
		return err
	}

	if out.Pretty {
		fmt.Println(style.Success("certificate renewed in %s", tildePath(rn.State)))
		fmt.Println(style.Note("restart the hub to serve it, then re-enroll peers (holt enroll <peer>)"))
	} else {
		fmt.Println("certificate renewed")
	}

	return nil
}

// confirm prints the warning and reads a yes/no from stdin. A closed or
// non-interactive stdin (EOF) counts as "no", so piping never renews by
// accident.
func (rn *Renew) confirm() bool {
	fmt.Println(style.Warn("renewing the certificate invalidates every join token already issued."))
	fmt.Println(style.Note("peers pinned the current certificate and will fail to attach until you"))
	fmt.Println(style.Note("re-enroll them. This cannot be undone."))
	fmt.Print("\nContinue? [y/N] ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		// EOF with nothing typed (piped/closed stdin): treat as no.
		fmt.Println()

		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
