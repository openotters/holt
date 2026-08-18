package commands

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	c "github.com/merlindorin/go-shared/pkg/cmd"

	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/pkg/revproxy"
)

// Info prints a snapshot of a hub: build, live counts, and the
// addresses an operator needs. It is the CLI view of what the console's
// status card shows.
type Info struct {
	adminConn
}

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
		Hint: routingHint(m.GetProxyRouting(), m.GetProxyDomain(), m.GetRouteHeader()),
	})

	if d := m.GetProxyDomain(); d != "" {
		rows = append(rows, style.BannerRow{
			Key: "peer domain", Value: "<peer>." + d, Hint: "subdomain routing",
		})
	}

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

// routingHint describes how the hub picks the target peer. A hub
// older than the strategy flag reports no routing, which always
// meant the header.
func routingHint(routing, domain, header string) string {
	byHeader := fmt.Sprintf("reach peers via the %s header", header)

	switch revproxy.Routing(routing) {
	case revproxy.RoutingSubdomain:
		return "reach peers via <peer>." + domain
	case revproxy.RoutingBoth:
		return byHeader + ", or <peer>." + domain
	case revproxy.RoutingHeader:
		return byHeader
	default:
		return byHeader
	}
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}

	return commit
}
