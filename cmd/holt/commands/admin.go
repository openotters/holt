package commands

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	c "github.com/merlindorin/go-shared/pkg/cmd"

	holtv1 "github.com/openotters/holt/api/v1"
	holtv1connect "github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/cmd/holt/internal/style"
)

// adminClient builds an Admin client against the hub's admin listener,
// using the connect protocol over HTTP/1.1. grpcurl can still reach
// the same handler over gRPC.
func adminClient(adminAddr string) holtv1connect.AdminClient {
	return holtv1connect.NewAdminClient(http.DefaultClient, "http://"+adminAddr)
}

// Ls lists live tunnels via the hub's Admin service.
type Ls struct {
	AdminAddr string `help:"Hub admin address." default:"127.0.0.1:7001"`
}

// Run prints the tunnel table.
func (l *Ls) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := adminClient(l.AdminAddr).ListTunnels(reqCtx, connect.NewRequest(&holtv1.ListTunnelsRequest{}))
	if err != nil {
		return fmt.Errorf("reaching hub at %s: %w", l.AdminAddr, err)
	}

	tunnels := resp.Msg.GetTunnels()
	if len(tunnels) == 0 {
		fmt.Println(style.Note("no peers attached"))

		return nil
	}

	rows := make([][]string, 0, len(tunnels))
	for _, t := range tunnels {
		attachedAt := time.Unix(t.GetAttachedAtUnix(), 0)
		attached := fmt.Sprintf("%s (%s ago)",
			attachedAt.Format("15:04:05"), time.Since(attachedAt).Round(time.Second))
		rows = append(rows, []string{t.GetPeer(), t.GetPeerVersion(), attached})
	}

	fmt.Println(style.Table([]string{"PEER", "VERSION", "ATTACHED"}, rows))

	return nil
}

// Kill forces a peer's tunnel closed via the hub's Admin service.
type Kill struct {
	Peer      string `arg:"" help:"Peer whose tunnel to close."`
	AdminAddr string `help:"Hub admin address." default:"127.0.0.1:7001"`
}

// Run stops the named tunnel.
func (k *Kill) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := adminClient(k.AdminAddr).StopTunnel(reqCtx,
		connect.NewRequest(&holtv1.StopTunnelRequest{Peer: k.Peer}))
	if err != nil {
		return fmt.Errorf("reaching hub at %s: %w", k.AdminAddr, err)
	}

	if resp.Msg.GetStopped() {
		fmt.Println(style.Success("stopped tunnel for %q", k.Peer))
	} else {
		fmt.Println(style.Note("no tunnel attached for %q", k.Peer))
	}

	return nil
}
