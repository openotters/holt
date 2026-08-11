import { createClient } from "@connectrpc/connect";
import { useEffect, useRef, useState } from "react";

import { Admin, type TunnelInfo, TunnelEvent_Kind } from "@/gen/v1/admin_pb";
import { transport } from "@/lib/transport";

// Subscribes to Admin.WatchTunnels (Connect server-streaming, plain
// HTTP, no websocket) and maintains the tunnel list CLIENT-SIDE from
// the snapshot + events, so a change never costs a ListTunnels
// roundtrip. The hello marker resets the set (a resubscribe replays a
// fresh snapshot). When the stream drops (hub restart, slow-watcher
// eviction) it resubscribes after a short delay; while down, the
// caller falls back to its regular query.
export function useLiveTunnels() {
	const [live, setLive] = useState(false);
	const [tunnels, setTunnels] = useState<TunnelInfo[]>([]);
	const [lastEventAt, setLastEventAt] = useState(0);
	const set = useRef(new Map<string, TunnelInfo>());

	useEffect(() => {
		const ac = new AbortController();
		const client = createClient(Admin, transport);

		const render = () => {
			setTunnels([...set.current.values()].sort((a, b) => a.peer.localeCompare(b.peer)));
			setLastEventAt(Date.now());
		};

		(async () => {
			while (!ac.signal.aborted) {
				try {
					for await (const ev of client.watchTunnels({}, { signal: ac.signal })) {
						switch (ev.kind) {
							case TunnelEvent_Kind.UNSPECIFIED:
								// hello: the subscription is up, snapshot follows
								set.current = new Map();
								setLive(true);
								render();
								break;
							case TunnelEvent_Kind.ATTACHED:
								if (ev.info) set.current.set(ev.info.peer, ev.info);
								render();
								break;
							case TunnelEvent_Kind.DETACHED:
								if (ev.info) set.current.delete(ev.info.peer);
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

	return { lastEventAt, live, tunnels };
}
