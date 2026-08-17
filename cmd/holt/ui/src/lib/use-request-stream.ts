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
};

// How many requests the panel keeps. Nothing is stored anywhere: this
// is a browser-side window over a live stream, and reloading the page
// starts it over (the hub replays only the handful it still holds).
const requestCap = 200;

// Subscribes to Admin.WatchRequests and keeps the newest requests,
// newest first. Resubscribes when the stream drops (hub restart, a
// client too slow to keep up), which replays whatever the hub still
// holds — hence the dedupe-free cap: a small replay overlap is
// harmless in a live view.
//
// An Unimplemented hub (one running without the request view) leaves
// supported false, so the panel can say so instead of spinning.
export function useLiveRequests() {
	const [requests, setRequests] = useState<LiveRequest[]>([]);
	const [live, setLive] = useState(false);
	const [supported, setSupported] = useState(true);
	const nextID = useRef(0);

	useEffect(() => {
		const ac = new AbortController();
		const client = createClient(Admin, transport);

		(async () => {
			while (!ac.signal.aborted) {
				try {
					setLive(true);
					for await (const ev of client.watchRequests({}, { signal: ac.signal })) {
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
				setLive(false);
				if (!ac.signal.aborted) {
					await new Promise((r) => setTimeout(r, 3000));
				}
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
			};
			setRequests((rs) => [entry, ...rs].slice(0, requestCap));
		}

		return () => ac.abort();
	}, []);

	return { live, requests, supported };
}
