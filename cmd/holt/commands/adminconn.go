package commands

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	holtv1connect "github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/cmd/holt/internal/config"
)

// adminConn is the shared connection surface of the admin commands
// (ls / kill / block / unblock): where the hub's admin API is and how
// to authenticate to whatever sits in front of it. Embed it in a
// command to get the flags and the client() helper.
type adminConn struct {
	AdminAddr string   `help:"Hub admin address (host:port, plaintext)." default:"127.0.0.1:7001"`
	AdminURL  string   `help:"Full admin URL, e.g. https://holt.example.com (overrides --admin-addr)." name:"admin-url"`
	Header    []string `help:"Extra request header 'Name: Value' (repeatable); overrides the profile's." name:"header"`
	Profile   string   `help:"Config profile to use (default: the file's default_profile)." env:"HOLT_PROFILE"`
	Config    string   `help:"Config file path (default: ~/.holt/config.yaml)." env:"HOLT_CONFIG" type:"path"`
}

// client resolves the endpoint and headers and returns an Admin client.
func (a adminConn) client() (holtv1connect.AdminClient, error) {
	e, err := a.endpoint()
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Transport: headerTransport{headers: e.headers, base: http.DefaultTransport}}

	return holtv1connect.NewAdminClient(httpClient, e.url), nil
}

// endpoint is the resolved connection: the base URL, the headers to
// send, and whether an endpoint was explicitly configured (a remote
// hub) versus the loopback default.
type endpoint struct {
	url     string
	headers map[string]string
	remote  bool
}

// endpoint applies precedence flag > env > profile > default for the
// URL, and merges the --header flags over the profile's headers.
func (a adminConn) endpoint() (endpoint, error) {
	cfg, err := config.Load(a.Config)
	if err != nil {
		return endpoint{}, err
	}

	prof := cfg.Pick(a.Profile)

	// An explicitly configured URL (flag, env, or profile) means a
	// remote hub; absent that, fall back to the loopback admin address.
	url := prof.AdminURL
	if env := os.Getenv("HOLT_ADMIN_URL"); env != "" {
		url = env
	}

	if a.AdminURL != "" {
		url = a.AdminURL
	}

	remote := url != ""
	if url == "" {
		url = "http://" + a.AdminAddr
	}

	headers := prof.ResolvedHeaders()
	for _, h := range a.Header {
		name, value, ok := splitHeader(h)
		if !ok {
			return endpoint{}, fmt.Errorf("invalid --header %q, want 'Name: Value'", h)
		}

		headers[name] = value
	}

	return endpoint{url: url, headers: headers, remote: remote}, nil
}

// httpClient builds an HTTP client that injects the resolved headers,
// for the bespoke /api/enroll endpoint (not a Connect RPC).
func (e endpoint) httpClient() *http.Client {
	return &http.Client{Transport: headerTransport{headers: e.headers, base: http.DefaultTransport}}
}

// splitHeader parses "Name: Value" into its parts.
func splitHeader(h string) (string, string, bool) {
	name, value, found := strings.Cut(h, ":")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)

	return name, value, found && name != ""
}

// headerTransport adds the configured headers to every request, so an
// authenticating proxy (Cloudflare Access, a gateway, a bearer token)
// in front of the hub lets the call through.
type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if len(t.headers) == 0 {
		return t.base.RoundTrip(r)
	}

	clone := r.Clone(r.Context())
	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}

	return t.base.RoundTrip(clone)
}
