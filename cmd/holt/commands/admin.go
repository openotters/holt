package commands

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"connectrpc.com/connect"
	c "github.com/merlindorin/go-shared/pkg/cmd"

	holtv1 "github.com/openotters/holt/api/v1"
	holtv1connect "github.com/openotters/holt/api/v1/holtv1connect"
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
		fmt.Println("no peers attached")

		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PEER\tVERSION\tATTACHED")

	for _, t := range tunnels {
		attached := time.Unix(t.GetAttachedAtUnix(), 0).Format(time.RFC3339)
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.GetPeer(), t.GetPeerVersion(), attached)
	}

	return w.Flush()
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
		fmt.Printf("stopped tunnel for %q\n", k.Peer)
	} else {
		fmt.Printf("no tunnel attached for %q\n", k.Peer)
	}

	return nil
}
