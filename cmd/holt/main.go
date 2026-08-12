// Command holt is the operator CLI for a reverse-tunnel hub: run the
// hub, enroll peers, and manage live tunnels. Peers authenticate with a
// JWT and the tunnel transport is encrypted with the hub's self-signed
// certificate, which the client pins.
//
// The peer side is a separate program — see cmd/starter-client, or
// write your own; both consume the enroll token.
//
//	holt hub                     # run the hub
//	holt enroll <peer>           # mint a join token for a peer
//	holt renew                   # regenerate the hub cert (invalidates tokens)
//	holt expose <addr> --token … # expose a local HTTP service through the hub
//	holt ls                      # list live tunnels
//	holt kill <peer>             # disconnect a tunnel (peer may reconnect)
//	holt block <peer>            # disconnect + ban the peer id
//	holt unblock <peer>          # lift a block
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
	c "github.com/merlindorin/go-shared/pkg/cmd"
	"go.uber.org/zap/zapcore"

	"github.com/openotters/holt/cmd/holt/commands"
	"github.com/openotters/holt/cmd/holt/internal/clog"
	"github.com/openotters/holt/cmd/holt/internal/style"
)

const (
	name        = "holt"
	description = "holt — reverse-tunnel hub & peers"
)

var (
	version     = "dev"
	commit      = "dirty"
	date        = "latest"
	buildSource = "source"
)

func main() {
	cli := CMD{
		Commons: &c.Commons{Version: c.NewVersion(name, version, commit, buildSource, date)},

		Hub:     &commands.Hub{},
		Enroll:  &commands.Enroll{},
		Renew:   &commands.Renew{},
		Expose:  &commands.Expose{},
		Info:    &commands.Info{},
		Ls:      &commands.Ls{},
		Kill:    &commands.Kill{},
		Block:   &commands.Block{},
		Unblock: &commands.Unblock{},
	}

	kctx := kong.Parse(
		&cli,
		kong.Name(name),
		kong.Description(description),
		kong.UsageOnError(),
		kong.DefaultEnvars("HOLT"),
		kong.Vars{"version": cli.Version.String()},
	)

	// The logger is built here, once, from the format flag: friendly
	// charm rendering by default, classic zap JSON for production
	// (--log-format json). Long-running commands receive it via kong.
	level, levelErr := zapcore.ParseLevel(cli.Level)
	kctx.FatalIfErrorf(levelErr)

	if cli.Development {
		level = zapcore.DebugLevel
	}

	logger, logErr := clog.New(cli.LogFormat, level)
	kctx.FatalIfErrorf(logErr)

	defer func() { _ = logger.Sync() }()

	// Signal-wired root context: Ctrl-C stops a running hub or peer
	// cleanly instead of a bare interrupt.
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	kctx.BindTo(runCtx, (*context.Context)(nil))
	kctx.FatalIfErrorf(kctx.Run(cli.Commons, logger, &style.Output{Pretty: cli.LogFormat != clog.FormatJSON}))
}

// CMD is the root command tree.
type CMD struct {
	*c.Commons

	ShowVersion kong.VersionFlag `name:"version" help:"Show version information and exit."`
	LogFormat   string           `help:"Log format: pretty for humans (default), json for production." enum:"pretty,json" default:"pretty"`

	Hub     *commands.Hub     `cmd:"" help:"Run the hub: TLS+JWT tunnel listener, Admin gRPC, and header-routed proxy."`
	Enroll  *commands.Enroll  `cmd:"" help:"Mint a join token (JWT + pinned hub cert) for a peer."`
	Renew   *commands.Renew   `cmd:"" help:"Regenerate the hub's TLS certificate (invalidates all existing join tokens)."`
	Expose  *commands.Expose  `cmd:"" help:"Expose a local HTTP service through the hub (ngrok style)."`
	Info    *commands.Info    `cmd:"" help:"Show a snapshot of the hub (build, counts, addresses, metrics)."`
	Ls      *commands.Ls      `cmd:"" aliases:"list" help:"List live tunnels via the hub's Admin service."`
	Kill    *commands.Kill    `cmd:"" help:"Force a peer's tunnel closed (it may reconnect)."`
	Block   *commands.Block   `cmd:"" help:"Ban a peer id and close its tunnel; every token for that id is refused until unblock."`
	Unblock *commands.Unblock `cmd:"" help:"Lift a peer's ban (tokens minted before the block work again until they expire)."`
}
