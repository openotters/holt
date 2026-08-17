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

// urlFor is where the request went, addressed the way a client could
// send it again: the peer's own hostname under subdomain routing, the
// proxy's address otherwise (where the route header names the peer).
export function urlFor(request: LiveRequest, peer: string, config: HubConfig): string {
	const subdomain = peerURL(peer, config.proxyDomain);
	const base = subdomain
		? subdomain.replace(/\/$/, "")
		: config.externalURL.replace(/\/$/, "") ||
			`http://${window.location.hostname || "127.0.0.1"}:${config.proxyPort}`;

	return base + request.path + (request.query ? `?${request.query}` : "");
}

// routeHeaderFor is the header a request needs to name its peer, or
// null when the URL already does (subdomain routing).
function routeHeaderFor(peer: string, config: HubConfig): [string, string] | null {
	return peerURL(peer, config.proxyDomain) ? null : [config.routeHeader, peer];
}

// The formats a request can be taken away in. Each rebuilds the same
// request — method, path, query, and the header that names the peer
// when the URL does not.
//
// None of them carries a body. The hub reports metadata only, it never
// keeps what was sent, so a command that invented one would be a lie
// dressed as a reproduction.
export type RequestFormat = {
	label: string;
	render: (request: LiveRequest, peer: string, config: HubConfig) => string;
};

export const requestFormats: RequestFormat[] = [
	{ label: "Copy URL", render: urlFor },
	{ label: "Copy as cURL", render: curlFor },
	{ label: "Copy as PowerShell", render: powershellFor },
	{ label: "Copy as fetch", render: fetchFor },
	{ label: "Copy as wget", render: wgetFor },
];

// curlFor rebuilds a request as a curl command that replays it through
// the hub. It is what you paste into a terminal to ask again.
export function curlFor(request: LiveRequest, peer: string, config: HubConfig): string {
	const parts = ["curl"];

	if (request.method !== "GET") {
		parts.push("-X", request.method);
	}

	const header = routeHeaderFor(peer, config);
	if (header) {
		parts.push("-H", `'${header[0]}: ${header[1]}'`);
	}

	parts.push(`'${urlFor(request, peer, config)}'`);

	return parts.join(" ");
}

// powershellFor is the same request for a Windows terminal.
function powershellFor(request: LiveRequest, peer: string, config: HubConfig): string {
	const parts = [`Invoke-WebRequest -Uri "${urlFor(request, peer, config)}"`];

	if (request.method !== "GET") {
		parts.push(`-Method ${request.method}`);
	}

	const header = routeHeaderFor(peer, config);
	if (header) {
		parts.push(`-Headers @{"${header[0]}"="${header[1]}"}`);
	}

	return parts.join(" ");
}

// fetchFor is the same request from a browser console or a script.
function fetchFor(request: LiveRequest, peer: string, config: HubConfig): string {
	const options: string[] = [];

	if (request.method !== "GET") {
		options.push(`  method: "${request.method}"`);
	}

	const header = routeHeaderFor(peer, config);
	if (header) {
		options.push(`  headers: { "${header[0]}": "${header[1]}" }`);
	}

	const url = `"${urlFor(request, peer, config)}"`;
	if (options.length === 0) {
		return `fetch(${url});`;
	}

	return `fetch(${url}, {\n${options.join(",\n")},\n});`;
}

// wgetFor is the same request for a box that has wget and not curl.
function wgetFor(request: LiveRequest, peer: string, config: HubConfig): string {
	const parts = ["wget"];

	if (request.method !== "GET") {
		parts.push(`--method=${request.method}`);
	}

	const header = routeHeaderFor(peer, config);
	if (header) {
		parts.push(`--header='${header[0]}: ${header[1]}'`);
	}

	parts.push(`'${urlFor(request, peer, config)}'`);

	return parts.join(" ");
}
