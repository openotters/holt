package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/cmd/holt/internal/style"
)

// RotateSecret regenerates the hub's JWT signing secret. Every join
// token already handed out was signed with the old secret, so rotating
// invalidates all of them at once: peers must be re-enrolled.
// Destructive, hence the confirmation.
type RotateSecret struct {
	State string `help:"Hub state directory (default: ~/.holt)." type:"path"`
	// Identity lives in the shared database when the hub was pointed
	// at one, and rotating there rotates for the whole fleet.
	DirectoryDSN string `help:"PostgreSQL DSN the hub stores its identity in, when it is not local." name:"directory-dsn" env:"HOLT_DIRECTORY_DSN"`
	Yes          bool   `help:"Skip the confirmation prompt (for automation)." short:"y"`
}

// Run rotates the signing secret after an interactive confirmation.
func (rs *RotateSecret) Run(ctx context.Context, _ *c.Commons, out *style.Output) error {
	if rs.State == "" {
		rs.State = defaultStateDir()
	}

	if !rs.Yes && !rs.confirm() {
		fmt.Println(style.Note("aborted, signing secret unchanged"))

		return nil
	}

	identity, closeIdentity, err := openSecretStore(ctx, rs.State, rs.DirectoryDSN)
	if err != nil {
		return err
	}

	defer closeIdentity()

	if sqlSecret, ok := identity.(*hubsecret.SQLStore); ok {
		if migErr := sqlSecret.Migrate(ctx); migErr != nil {
			return migErr
		}
	}

	_, shared := identity.(*hubsecret.SQLStore)

	if _, rotateErr := identity.Rotate(ctx); rotateErr != nil {
		return rotateErr
	}

	if out.Pretty {
		fmt.Println(style.Success("signing secret rotated in %s", tildePath(identity.Describe())))

		// A hub reading its identity from a shared backend picks the
		// rotation up on its own; a file-backed one is only read at
		// boot, so that one needs the restart.
		if shared {
			fmt.Println(style.Note(
				"running hubs adopt it within 30s and close their tunnels; re-enroll peers (holt enroll <peer>)"))
		} else {
			fmt.Println(style.Note("restart the hub to load it, then re-enroll peers (holt enroll <peer>)"))
		}
	} else {
		fmt.Println("signing secret rotated")
	}

	return nil
}

// confirm prints the warning and reads a yes/no from stdin. A closed or
// non-interactive stdin (EOF) counts as "no", so piping never rotates by
// accident.
func (rs *RotateSecret) confirm() bool {
	fmt.Println(style.Warn("rotating the signing secret invalidates every join token already issued."))
	fmt.Println(style.Note("peers will fail to attach until you re-enroll them. This cannot be undone."))
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
