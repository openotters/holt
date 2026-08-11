import { createClient } from "@connectrpc/connect";
import { useEffect, useRef, useState } from "react";

import { Admin } from "@/gen/v1/admin_pb";
import { transport } from "@/lib/transport";

// Subscribes to Admin.WatchTunnels (Connect server-streaming, plain
// HTTP, no websocket) and reports liveness. onEvent fires for every
// event, snapshot included; the caller refreshes its queries there.
// When the stream drops (hub restart, slow-watcher eviction) it
// resubscribes after a short delay; the regular react-query polling
// keeps working as the fallback in between.
export function useTunnelStream(onEvent: () => void) {
	const [live, setLive] = useState(false);
	const cb = useRef(onEvent);
	cb.current = onEvent;

	useEffect(() => {
		const ac = new AbortController();
		const client = createClient(Admin, transport);

		(async () => {
			while (!ac.signal.aborted) {
				try {
					for await (const _ev of client.watchTunnels({}, { signal: ac.signal })) {
						setLive(true);
						cb.current();
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

	return live;
}
