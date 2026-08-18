import { createClient } from "@connectrpc/connect";
import { useEffect, useRef, useState } from "react";

import { Admin, type RequestEvent } from "@/gen/v1/admin_pb";
import { transport } from "@/lib/transport";

// One request the hub carried, as the panel shows it.
export type LiveRequest = {
	id: number; // client-side key; the hub sends no id
	at: number; // hub clock, ms
	peer: string;
	method: string;
	path: string;
	status: number; // 0 when the request never got a response
	tookMs: number;
	query: string;
	host: string;
	proto: string;
	remoteAddr: string;
	userAgent: string;
	requestBytes: number; // -1 when the request did not declare one
	responseBytes: number;
	requestHeaders: Record<string, string>;
	responseHeaders: Record<string, string>;
	requestBody: CapturedBody | null;
	responseBody: CapturedBody | null;
};

// CapturedBody is what the hub kept of a payload: a bounded prefix, or
// the reason it kept nothing.
export type CapturedBody = {
	content: string;
	size: number;
	truncated: boolean;
	skipped: string; // "", "disabled" or "binary"
};

// How many requests the view keeps. Nothing is stored anywhere: this
// is a browser-side window over a live stream, and closing it starts
// over (the hub replays only the handful it still holds).
const requestCap = 200;

// Subscribes to Admin.WatchRequests for one peer and keeps the newest
// requests, newest first. The peer goes to the hub, which filters
// there: a console watching one peer of a fleet is never sent the
// rest. Passing "" watches every peer.
//
// Resubscribes when the stream drops (hub restart, a client too slow
// to keep up), which replays whatever the hub still holds — a small
// overlap is harmless in a live view.
//
// An Unimplemented hub (one older than the request view) leaves
// supported false, so the caller can say so instead of spinning.
// asBody decodes a captured payload. The hub sends bytes, since a
// body is whatever the client sent and not necessarily text; anything
// that is not valid UTF-8 comes back with replacement characters
// rather than failing the row.
function asBody(body: RequestEvent["requestBody"]): CapturedBody | null {
	if (!body) return null;
	return {
		content: new TextDecoder().decode(body.content),
		size: Number(body.size),
		truncated: body.truncated,
		skipped: body.skipped,
	};
}

export function useLiveRequests(peer: string) {
	const [requests, setRequests] = useState<LiveRequest[]>([]);
	const [live, setLive] = useState(false);
	const [supported, setSupported] = useState(true);
	const nextID = useRef(0);

	useEffect(() => {
		const ac = new AbortController();
		const client = createClient(Admin, transport);

		// A new peer is a new view: nothing from the previous one
		// belongs in it.
		setRequests([]);

		(async () => {
			while (!ac.signal.aborted) {
				try {
					setLive(true);
					for await (const ev of client.watchRequests({ peer }, { signal: ac.signal })) {
						push(ev);
					}
				} catch (err) {
					// Unimplemented is an answer, not a failure: this hub
					// does not carry the view, so stop asking.
					if (!ac.signal.aborted && String(err).includes("unimplemented")) {
						setSupported(false);
						setLive(false);
						return;
					}
				}
				// After an abort this closure is the previous peer's stream
				// winding down; its setLive(false) would land after the next
				// peer's setLive(true).
				if (ac.signal.aborted) return;
				setLive(false);
				await new Promise((r) => setTimeout(r, 3000));
			}
		})();

		function push(ev: RequestEvent) {
			const entry: LiveRequest = {
				id: nextID.current++,
				at: Number(ev.atUnixMillis),
				peer: ev.peer,
				method: ev.method,
				path: ev.path,
				status: ev.status,
				tookMs: Number(ev.durationUs) / 1000,
				query: ev.query,
				host: ev.host,
				proto: ev.proto,
				remoteAddr: ev.remoteAddr,
				userAgent: ev.userAgent,
				requestBytes: Number(ev.requestBytes),
				responseBytes: Number(ev.responseBytes),
				requestHeaders: ev.requestHeaders,
				responseHeaders: ev.responseHeaders,
				requestBody: asBody(ev.requestBody),
				responseBody: asBody(ev.responseBody),
			};
			setRequests((rs) => [entry, ...rs].slice(0, requestCap));
		}

		return () => ac.abort();
	}, [peer]);

	return { live, requests, supported };
}
