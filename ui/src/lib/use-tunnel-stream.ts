import { createClient } from "@connectrpc/connect";
import { useEffect, useRef, useState } from "react";

import { Admin, type TunnelInfo, TunnelEvent_Kind } from "@/gen/v1/admin_pb";
import { transport } from "@/lib/transport";

// One attach/detach observed by the stream, for the activity feed.
export type TunnelActivity = {
	at: number; // client clock, ms
	kind: "attached" | "detached";
	peer: string;
	reason?: string; // detach only
};

const activityCap = 50;

// Subscribes to Admin.WatchTunnels (Connect server-streaming, plain
// HTTP, no websocket) and maintains the tunnel list CLIENT-SIDE from
// the snapshot + events, so a change never costs a ListTunnels
// roundtrip. The hello marker resets the set (a resubscribe replays a
// fresh snapshot). When the stream drops (hub restart, slow-watcher
// eviction) it resubscribes after a short delay; while down, the
// caller falls back to its regular query.
//
// It also keeps the last attach/detach events as an activity log.
// Snapshot replays (the ATTACHED burst right after hello) are not
// activity, they are state, so events in the first second after
// hello only seed the set and stay out of the log.
export function useLiveTunnels() {
	const [live, setLive] = useState(false);
	const [tunnels, setTunnels] = useState<TunnelInfo[]>([]);
	const [activity, setActivity] = useState<TunnelActivity[]>([]);
	const [lastEventAt, setLastEventAt] = useState(0);
	const set = useRef(new Map<string, TunnelInfo>());
	const snapshotUntil = useRef(0);

	useEffect(() => {
		const ac = new AbortController();
		const client = createClient(Admin, transport);

		const render = () => {
			setTunnels([...set.current.values()].sort((a, b) => a.peer.localeCompare(b.peer)));
			setLastEventAt(Date.now());
		};

		const log = (entry: TunnelActivity) => {
			if (Date.now() < snapshotUntil.current) return;
			setActivity((a) => [entry, ...a].slice(0, activityCap));
		};

		(async () => {
			while (!ac.signal.aborted) {
				try {
					for await (const ev of client.watchTunnels({}, { signal: ac.signal })) {
						switch (ev.kind) {
							case TunnelEvent_Kind.UNSPECIFIED:
								// hello: the subscription is up, snapshot follows
								set.current = new Map();
								snapshotUntil.current = Date.now() + 1000;
								setLive(true);
								render();
								break;
							case TunnelEvent_Kind.ATTACHED:
								if (ev.info) {
									// Subscribe and snapshot overlap by design; a
									// duplicate ATTACHED is state, not news.
									if (!set.current.has(ev.info.peer)) {
										log({ at: Date.now(), kind: "attached", peer: ev.info.peer });
									}
									set.current.set(ev.info.peer, ev.info);
								}
								render();
								break;
							case TunnelEvent_Kind.DETACHED:
								if (ev.info) {
									set.current.delete(ev.info.peer);
									log({
										at: Date.now(),
										kind: "detached",
										peer: ev.info.peer,
										reason: ev.reason || undefined,
									});
								}
								render();
								break;
						}
					}
				} catch {
					// stream dropped; resubscribe below
				}
				setLive(false);
				if (!ac.signal.aborted) {
					await new Promise((r) => setTimeout(r, 3000));
				}
			}
		})();

		return () => ac.abort();
	}, []);

	return { activity, lastEventAt, live, tunnels };
}
