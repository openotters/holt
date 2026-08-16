package commands

import (
	"bytes"
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/pkg/registry"
)

// identityPollInterval is how often a hub re-reads the shared signing
// secret. Rotation is rare and deliberate, so this only has to be
// quick enough that a fleet converges within a coffee-length window,
// not instant.
const identityPollInterval = 30 * time.Second

// watchIdentity keeps this hub's signing secret in step with the
// shared backend, so rotating on ANY hub rotates the fleet.
//
// Without it, a rotation on hub A leaves hub B verifying with the old
// secret: tokens A mints are refused by B, and tokens the operator was
// told are dead still work on B. The poll closes that window. It only
// runs on a shared backend — a file-backed hub is the only writer of
// its own secret, and the console already swaps that one in place.
func watchIdentity(
	ctx context.Context, identity hubsecret.Store, secrets secretHolder,
	tunnels tunnelStopper, logger *zap.Logger,
) {
	if _, shared := identity.(*hubsecret.SQLStore); !shared {
		return
	}

	go func() {
		ticker := time.NewTicker(identityPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				adoptRotatedSecret(ctx, identity, secrets, tunnels, logger)
			}
		}
	}()
}

// adoptRotatedSecret swaps in the stored secret when another hub has
// rotated it. A read that fails is left alone: the backend being
// briefly unreachable is not a reason to stop trusting the secret this
// hub already holds.
func adoptRotatedSecret(
	ctx context.Context, identity hubsecret.Store, secrets secretHolder,
	tunnels tunnelStopper, logger *zap.Logger,
) {
	readCtx, cancel := context.WithTimeout(ctx, identityReadTimeout)
	defer cancel()

	stored, err := identity.Load(readCtx)
	if err != nil {
		logger.Debug("could not re-read the shared signing secret", zap.Error(err))

		return
	}

	if bytes.Equal(stored, secrets.Get()) {
		return
	}

	secrets.Set(stored)

	// Rotation means every token is dead, so the tunnels authenticated
	// with the old one go too — the same thing the rotating hub did to
	// its own. token-revoked is terminal, so peers stop redialing until
	// they are re-enrolled, which is the point of rotating.
	closed := tunnels.CountTunnels()
	tunnels.StopAllTunnels(registry.ReasonTokenRevoked)

	logger.Warn("signing secret rotated on another hub; adopted it, tokens invalidated, tunnels closed",
		zap.Int("closed_tunnels", closed))
}

// identityReadTimeout bounds the poll so a wedged backend cannot pin
// the goroutine.
const identityReadTimeout = 5 * time.Second

// secretHolder is the live signing secret; *jwtauth.Secret satisfies it.
type secretHolder interface {
	Get() []byte
	Set([]byte)
}

// tunnelStopper closes tunnels when the secret underneath them is
// revoked; *registry.Registry satisfies it.
type tunnelStopper interface {
	CountTunnels() int
	StopAllTunnels(reason string)
}
