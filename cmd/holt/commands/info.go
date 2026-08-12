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

// Info prints a snapshot of a hub: build, live counts, and the
// addresses an operator needs. It is the CLI view of what the console's
// status card shows.
type Info struct {
	adminConn
}

// Run fetches and prints the hub info.
func (i *Info) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := i.client()
	if err != nil {
		return err
	}

	ep, err := i.endpoint()
	if err != nil {
		return err
	}

	resp, err := client.Info(reqCtx, connect.NewRequest(&holtv1.InfoRequest{}))
	if err != nil {
		return fmt.Errorf("reaching hub: %w", err)
	}

	fmt.Print(infoBanner(ep.url, resp.Msg))

	return nil
}

// infoBanner renders the hub info as a labelled block, matching the
// welcome banner's look.
func infoBanner(endpoint string, m *holtv1.InfoResponse) string {
	heading := "holt"
	if v := m.GetVersion(); v != "" {
		heading = "holt " + v
		if commit := m.GetCommit(); commit != "" {
			heading += " (" + shortCommit(commit) + ")"
		}
	}

	rows := []style.BannerRow{
		{Key: "endpoint", Value: endpoint},
		{Key: "tunnels", Value: fmt.Sprintf("%d", m.GetTunnels()), Hint: "live"},
		{Key: "blocked", Value: fmt.Sprintf("%d", m.GetBlocked()), Hint: "banned peer ids"},
	}

	if a := m.GetAdvertiseAddr(); a != "" {
		rows = append(rows, style.BannerRow{Key: "advertise", Value: a, Hint: "address stamped into tokens"})
	}

	rows = append(rows, style.BannerRow{
		Key: "proxy", Value: m.GetProxyAddr(),
		Hint: fmt.Sprintf("reach peers via the %s header", m.GetRouteHeader()),
	})

	if a := m.GetMetricsAddr(); a != "" {
		rows = append(rows, style.BannerRow{Key: "metrics", Value: a + "/metrics", Hint: "prometheus"})
	}

	if u := m.GetExternalUrl(); u != "" {
		rows = append(rows, style.BannerRow{Key: "external", Value: u, Hint: "public proxy URL"})
	}

	if ttl := m.GetTokenTtlSeconds(); ttl > 0 {
		rows = append(rows, style.BannerRow{
			Key: "token ttl", Value: (time.Duration(ttl) * time.Second).String(),
			Hint: "lifetime of minted tokens",
		})
	}

	return style.Banner(heading, rows, "")
}

// shortCommit trims a commit hash to the first 7 characters.
func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}

	return commit
}
