package commands

import (
	"fmt"
	"net/http"
	"strings"

	holtv1connect "github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/cmd/holt/internal/config"
)

// Every configurable value follows the same precedence:
//
//	flag > env (HOLT_*) > profile (~/.holt/config.yaml) > built-in default
//
// kong merges each flag with its HOLT_* env into the struct field (the
// flag winning), so resolution here only layers the profile and the
// default on top with coalesce().

// adminConn is the shared connection surface of the admin commands
// (info / ls / kill / block / unblock, and enroll): where the hub's
// admin API is and how to authenticate to whatever sits in front of it.
// Embed it in a command to get the flags and the client() helper.
type adminConn struct {
	AdminAddr string   `help:"Hub admin address (host:port, plaintext; default 127.0.0.1:7201)." env:"HOLT_ADMIN_ADDR"`
	AdminURL  string   `help:"Full admin URL, e.g. https://holt.example.com (overrides --admin-addr)." name:"admin-url" env:"HOLT_ADMIN_URL"`
	Header    []string `help:"Extra request header 'Name: Value' (repeatable); overrides the profile's." name:"header" env:"HOLT_HEADER"`
	Profile   string   `help:"Config profile to use (default: the file's default_profile)." env:"HOLT_PROFILE"`
	Config    string   `help:"Config file path (default: ~/.holt/config.yaml)." env:"HOLT_CONFIG" type:"path"`
}

// coalesce returns the first non-empty value, the shared primitive of
// the flag > env > profile > default precedence (the flag+env part is
// already folded into the first argument by kong).
func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return ""
}

// httpAddr turns a host:port into an http:// URL, or "" when empty.
func httpAddr(addr string) string {
	if addr == "" {
		return ""
	}

	return "http://" + addr
}

// pointsAtAnotherHub reports that a flag or env aimed this command at a
// different hub than the one the profile describes. The profile's other
// hub-specific settings (its tunnel_url, its headers) describe ITS hub,
// so they stop applying when the endpoint is redirected: a token minted
// at hub B must not advertise hub A's tunnel URL, and hub A's
// credentials must not be sent to hub B.
//
// A profile with no admin_url describes no particular endpoint, so its
// settings keep applying — that is the "mint locally on the hub
// machine, advertise the public URL" setup.
func (a adminConn) pointsAtAnotherHub(prof config.Profile) bool {
	override := coalesce(a.AdminURL, httpAddr(a.AdminAddr))
	if override == "" || prof.AdminURL == "" {
		return false
	}

	return !sameEndpoint(override, prof.AdminURL)
}

// sameEndpoint compares two admin URLs the way the CLI reaches them, so
// a trailing slash is not a different hub.
func sameEndpoint(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
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

// endpoint resolves the admin URL and headers. --admin-url wins, then
// --admin-addr (as http://), then the profile's admin_url; any of those
// means a remote hub was named, otherwise it falls back to the loopback
// default (and stays "local" for enroll).
func (a adminConn) endpoint() (endpoint, error) {
	prof, err := a.profile()
	if err != nil {
		return endpoint{}, err
	}

	url := coalesce(a.AdminURL, httpAddr(a.AdminAddr), prof.AdminURL)
	remote := url != ""

	if url == "" {
		url = "http://127.0.0.1:7201"
	}

	// The profile's headers authenticate the profile's hub: an Access
	// service token, a bearer, whatever sits in front of it. Aiming the
	// endpoint at another hub must not hand them to it, so they go with
	// the profile. --header still applies, being explicit.
	headers := map[string]string{}
	if !a.pointsAtAnotherHub(prof) {
		headers = prof.ResolvedHeaders()
	}

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

// profile loads and selects the configured profile (empty when there is
// no config or no match).
func (a adminConn) profile() (config.Profile, error) {
	cfg, err := config.Load(a.Config)
	if err != nil {
		return config.Profile{}, err
	}

	return cfg.Pick(a.Profile), nil
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
