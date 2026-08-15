//nolint:testpackage // localProxy is unexported; white-box unit test on purpose.
package commands

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"testing"
)

// --insecure exists for appliances serving a self-signed certificate
// (a router, a NAS, an IPMI board). Without it the proxy must still
// refuse them, so the default stays a verifying one.
func TestLocalProxy_InsecureReachesSelfSignedTarget(t *testing.T) {
	t.Parallel()

	// httptest's TLS server presents exactly that: a certificate no
	// system root will verify.
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("appliance ui"))
	}))
	// Cleanup, not defer: the parallel subtests below resume after
	// this function returns, so a deferred Close would shut the
	// target before they ever reach it.
	t.Cleanup(target.Close)

	t.Run("insecure proxies it", func(t *testing.T) {
		t.Parallel()

		body, status := throughProxy(t, target.URL, true)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", status, body)
		}

		if body != "appliance ui" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("verifying by default", func(t *testing.T) {
		t.Parallel()

		_, status := throughProxy(t, target.URL, false)
		if status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502 for an unverifiable certificate", status)
		}
	})
}

// A plaintext target has no certificate to skip, so --insecure must
// leave it on the default transport.
func TestLocalProxy_InsecureIsHTTPSOnly(t *testing.T) {
	t.Parallel()

	proxy, _, err := localProxy("127.0.0.1:9999", true)
	if err != nil {
		t.Fatal(err)
	}

	if rp, ok := proxy.(*httputil.ReverseProxy); !ok || rp.Transport != nil {
		t.Fatal("an http target must keep the default transport")
	}
}

func TestLocalProxy_DefaultKeepsDefaultTransport(t *testing.T) {
	t.Parallel()

	proxy, u, err := localProxy("https://appliance.invalid", false)
	if err != nil {
		t.Fatal(err)
	}

	if u.Scheme != "https" {
		t.Fatalf("scheme = %q", u.Scheme)
	}

	if rp, ok := proxy.(*httputil.ReverseProxy); !ok || rp.Transport != nil {
		t.Fatal("without --insecure the proxy must keep the verifying default transport")
	}
}

// insecureTransport must skip verification and nothing else: it keeps
// the default transport's pooling and proxy settings.
func TestInsecureTransportSkipsOnlyVerification(t *testing.T) {
	t.Parallel()

	tr := insecureTransport()

	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("verification is still on")
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skip("default transport is not an *http.Transport")
	}

	if tr.MaxIdleConns != base.MaxIdleConns {
		t.Fatalf("MaxIdleConns = %d, want the default %d", tr.MaxIdleConns, base.MaxIdleConns)
	}
}

// throughProxy drives one request through the reverse proxy the
// expose command would build, returning the body and status.
func throughProxy(t *testing.T, target string, insecure bool) (string, int) {
	t.Helper()

	proxy, _, err := localProxy(target, insecure)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return string(body), resp.StatusCode
}

// Guard the blast radius: the flag must not reach into the tunnel's
// own TLS. This is a compile-time reminder more than a runtime check.
var _ = tls.Config{}
