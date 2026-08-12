package commands

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	c "github.com/merlindorin/go-shared/pkg/cmd"

	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/cmd/holt/internal/style"
)

// Ls lists live tunnels via the hub's Admin service.
type Ls struct {
	adminConn
}

// Run prints the tunnel table.
func (l *Ls) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := l.client()
	if err != nil {
		return err
	}

	resp, err := client.ListTunnels(reqCtx, connect.NewRequest(&holtv1.ListTunnelsRequest{}))
	if err != nil {
		return fmt.Errorf("reaching hub: %w", err)
	}

	tunnels := resp.Msg.GetTunnels()
	if len(tunnels) == 0 {
		fmt.Println(style.Note("no peers attached"))

		return nil
	}

	rows := make([][]string, 0, len(tunnels))
	for _, t := range tunnels {
		age := kubeAge(time.Since(time.Unix(t.GetAttachedAtUnix(), 0)))
		rows = append(rows, []string{t.GetPeer(), t.GetPeerVersion(), age})
	}

	fmt.Println(style.List([]string{"PEER", "VERSION", "AGE"}, rows))

	return nil
}

// kubeAge formats a duration the way `kubectl get` prints AGE: a short
// single unit (seconds, minutes, hours) up to a day, then days.
func kubeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Kill forces a peer's tunnel closed via the hub's Admin service.
type Kill struct {
	Peer string `arg:"" help:"Peer whose tunnel to close."`
	adminConn
}

// Run stops the named tunnel.
func (k *Kill) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := k.client()
	if err != nil {
		return err
	}

	resp, err := client.StopTunnel(reqCtx,
		connect.NewRequest(&holtv1.StopTunnelRequest{Peer: k.Peer}))
	if err != nil {
		return fmt.Errorf("reaching hub: %w", err)
	}

	if resp.Msg.GetStopped() {
		fmt.Println(style.Success("stopped tunnel for %q", k.Peer))
	} else {
		fmt.Println(style.Note("no tunnel attached for %q", k.Peer))
	}

	return nil
}
