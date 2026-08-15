package httpsrv

import (
	"net"
	"net/http"
	"strings"
)

// healthPath is exempt from the host check: probes carry the pod IP as
// Host and the endpoint returns no data.
const healthPath = "/healthz"

// HostGuard rejects requests whose Host header is not allow-listed,
// which is what stops a DNS-rebinding page from driving the plaintext,
// loopback-served console and its token-minting endpoint. Loopback
// names are always allowed; a deployment exposed through a proxy adds
// its public hostname. A single "*" entry disables the check.
func HostGuard(allowed []string, next http.Handler) http.Handler {
	allow := map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}
	wildcard := false

	for _, h := range allowed {
		if h == "*" {
			wildcard = true
		}

		if h != "" {
			allow[strings.ToLower(h)] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == healthPath {
			next.ServeHTTP(w, r)

			return
		}

		if !wildcard {
			host := r.Host
			if hh, _, err := net.SplitHostPort(host); err == nil {
				host = hh
			}

			if !allow[strings.ToLower(host)] {
				http.Error(w, "forbidden: host not allowed (set --allowed-host)", http.StatusForbidden)

				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
