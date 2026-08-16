package commands

import (
	"net"
	"strings"

	"go.uber.org/zap"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/hubapi"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/pkg/admin"
	"github.com/openotters/holt/pkg/revproxy"
)

// routing is the proxy strategy the flags select.
func (h *Hub) routing() revproxy.Routing { return revproxy.Routing(h.ProxyRouting) }

// advertiseURL is the tunnel URL stamped into tokens (what peers dial):
// the operator override if set, otherwise ws://<bind address>. A value
// without a scheme is assumed ws (plaintext), so peers get a
// well-formed URL and the scheme drives their transport.
func (h *Hub) advertiseURL() string {
	adv := h.AdvertiseAddr
	if adv == "" {
		adv = h.TunnelAddr
	}

	if !strings.Contains(adv, "://") {
		adv = "ws://" + adv
	}

	return adv
}

// adminInfo builds the static hub metadata the Admin Info RPC reports.
func (h *Hub) adminInfo(commons *c.Commons) admin.HubInfo {
	return admin.HubInfo{
		Version:       commons.Version.Version(),
		Commit:        commons.Version.Commit(),
		AdvertiseAddr: h.advertiseURL(),
		ProxyAddr:     h.ProxyAddr,
		RouteHeader:   revproxy.RouteHeader,
		MetricsAddr:   h.metricsAddr(),
		ExternalURL:   strings.TrimRight(h.ExternalURL, "/"),
		TokenTTL:      h.TokenTTL,
		ProxyRouting:  h.ProxyRouting,
		ProxyDomain:   h.ProxyDomain,
	}
}

// consoleConfig is what the web console reads at startup to build the
// commands it shows.
func (h *Hub) consoleConfig() hubapi.Config {
	return hubapi.Config{
		RouteHeader:  revproxy.RouteHeader,
		ProxyPort:    portOf(h.ProxyAddr),
		ExternalURL:  strings.TrimRight(h.ExternalURL, "/"),
		TunnelURL:    h.advertiseURL(),
		MetricsPort:  portOf(h.metricsAddr()),
		ProxyRouting: h.ProxyRouting,
		ProxyDomain:  h.ProxyDomain,
	}
}

// metricsAddr is the metrics listener address, or empty when metrics
// are off — which is how both the console and the Info RPC say "not
// served".
func (h *Hub) metricsAddr() string {
	if !h.Metrics {
		return ""
	}

	return h.MetricsAddr
}

// portOf returns the port of a host:port address, or the whole string
// if it has no port.
func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}

	return addr
}

// logFields is the "hub up" structured line for --log-format json.
func (h *Hub) logFields() []zap.Field {
	fields := []zap.Field{
		zap.String("tunnel", h.TunnelAddr),
		zap.String("admin", h.AdminAddr),
		zap.String("proxy", h.ProxyAddr),
	}

	if h.DirectoryDSN != "" {
		fields = append(fields, zap.String("directory", redactDSN(h.DirectoryDSN)))
	}

	if h.UI {
		fields = append(fields, zap.String("console", "http://"+h.AdminAddr+"/"))
	}

	if h.Metrics {
		fields = append(fields, zap.String("metrics", "http://"+h.MetricsAddr+"/metrics"))
	}

	return fields
}

// welcomeBanner renders the pretty startup block with the addresses
// and a first-step hint.
func (h *Hub) welcomeBanner() string {
	rows := []style.BannerRow{
		{Key: "tunnel", Value: h.TunnelAddr, Hint: "peers attach here (JWT auth; put TLS in front)"},
	}

	if h.AdvertiseAddr != "" {
		rows = append(rows,
			style.BannerRow{Key: "advertise", Value: h.advertiseURL(), Hint: "URL stamped into tokens"})
	}

	rows = append(rows, style.BannerRow{Key: "admin", Value: h.AdminAddr, Hint: "holt ls / kill / block"})

	if h.UI {
		rows = append(rows,
			style.BannerRow{Key: "console", Value: "http://" + h.AdminAddr + "/", Hint: "web console"})
	}

	rows = append(rows, style.BannerRow{Key: "proxy", Value: h.ProxyAddr, Hint: h.proxyHint()})

	if h.Metrics {
		rows = append(rows,
			style.BannerRow{Key: "metrics", Value: h.MetricsAddr + "/metrics", Hint: "prometheus metrics"})
	}

	if h.ExternalURL != "" {
		rows = append(rows, style.BannerRow{
			Key:   "external",
			Value: strings.TrimRight(h.ExternalURL, "/"),
			Hint:  "public URL peers are reached at",
		})
	}

	rows = append(rows, style.BannerRow{Key: "state", Value: tildePath(h.State), Hint: "JWT secret, blocklist"})

	if h.DirectoryDSN != "" {
		rows = append(rows, style.BannerRow{
			Key:   "directory",
			Value: redactDSN(h.DirectoryDSN),
			Hint:  "shared presence (postgres)",
		})
	}

	return style.Banner("holt is up", rows, "enroll your first peer:  holt enroll <name>")
}

// proxyHint tells the operator how to reach a peer through the proxy,
// in the terms the configured routing actually accepts.
func (h *Hub) proxyHint() string {
	switch h.routing() {
	case revproxy.RoutingSubdomain:
		return "reach peers: <peer>." + h.ProxyDomain
	case revproxy.RoutingBoth:
		return "reach peers: <peer>." + h.ProxyDomain + " (or the " + revproxy.RouteHeader + " header)"
	case revproxy.RoutingHeader:
		return "reach peers: curl -H '" + revproxy.RouteHeader + ": <peer>'"
	default:
		return "reach peers: curl -H '" + revproxy.RouteHeader + ": <peer>'"
	}
}
