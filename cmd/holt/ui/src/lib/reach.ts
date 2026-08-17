import type { LiveRequest } from "@/lib/use-request-stream";

// HubConfig is the part of /api/config that says how to reach a peer:
// where the proxy is, and how it picks the target.
export type HubConfig = {
	routeHeader: string;
	proxyPort: string;
	externalURL: string;
	proxyDomain: string;
	proxyRouting: string;
};

// peerURL is a peer's own hostname under the proxy domain, or "" when
// the hub does not route by subdomain (there is then no URL that names
// the peer, only the header).
export function peerURL(peer: string, proxyDomain: string): string {
	return proxyDomain ? `https://${peer}.${proxyDomain}/` : "";
}

// curlFor rebuilds a request as a curl command that replays it through
// the hub: the same method, path and query, addressed the way this hub
// routes. It is what you paste into a terminal to ask again.
//
// The body is not part of it. The hub reports metadata only — it never
// keeps what was sent — so a command that invented one would be a lie
// dressed as a reproduction.
export function curlFor(request: LiveRequest, peer: string, config: HubConfig): string {
	const parts = ["curl"];

	if (request.method !== "GET") {
		parts.push("-X", request.method);
	}

	// The subdomain form needs no header, so prefer it; otherwise name
	// the peer explicitly against whatever base URL is reachable.
	const subdomain = peerURL(peer, config.proxyDomain);
	const base = subdomain
		? subdomain.replace(/\/$/, "")
		: config.externalURL.replace(/\/$/, "") ||
			`http://${window.location.hostname || "127.0.0.1"}:${config.proxyPort}`;

	if (!subdomain) {
		parts.push("-H", `'${config.routeHeader}: ${peer}'`);
	}

	const path = request.path + (request.query ? `?${request.query}` : "");

	parts.push(`'${base}${path}'`);

	return parts.join(" ");
}
