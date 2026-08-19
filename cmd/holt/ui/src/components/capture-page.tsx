import { Inbox, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { CopyField } from "@/components/copy-field";
import { TrafficPanel } from "@/components/traffic-panel";
import { Button } from "@/components/ui/button";
import { type HubConfig, peerURL } from "@/lib/reach";

// A capture endpoint as /api/captures reports it (RFC 3339 dates).
type Capture = { peer: string; createdAt: string; expiresAt: string };

// CapturePage is the request inspector: capture endpoints on the left,
// the selected one's live traffic on the right, the same TrafficPanel
// the tunnels page opens per peer, since a capture endpoint IS a peer.
export function CapturePage({ config }: { config: HubConfig }) {
	const [captures, setCaptures] = useState<Capture[]>([]);
	const [selected, setSelected] = useState<string | null>(null);
	const [creating, setCreating] = useState(false);
	const [loaded, setLoaded] = useState(false);

	const refresh = useCallback(async () => {
		try {
			const res = await fetch("/api/captures");
			if (!res.ok) return;
			const list = ((await res.json()) as { captures?: Capture[] }).captures ?? [];
			setCaptures(list);
			setLoaded(true);
			// Keep the selection while its endpoint lives; else the first.
			setSelected((s) => (s && list.some((c) => c.peer === s) ? s : (list[0]?.peer ?? null)));
		} catch {
			// keep the last known list; the next poll retries
		}
	}, []);

	// A slow poll keeps the list and countdowns honest with hub-side expiry.
	useEffect(() => {
		refresh();
		const t = setInterval(refresh, 15_000);
		return () => clearInterval(t);
	}, [refresh]);

	const create = async () => {
		setCreating(true);
		try {
			const res = await fetch("/api/captures", { method: "POST" });
			if (!res.ok) throw new Error(await res.text());
			const bin = (await res.json()) as Capture;
			setSelected(bin.peer);
			await refresh();
		} catch (e) {
			toast.error("Create failed", { description: e instanceof Error ? e.message : String(e) });
		} finally {
			setCreating(false);
		}
	};

	const remove = async (peer: string) => {
		const res = await fetch(`/api/captures/${encodeURIComponent(peer)}`, { method: "DELETE" });
		if (!res.ok && res.status !== 404) {
			toast.error("Delete failed", { description: await res.text() });
		}
		await refresh();
	};

	const current = captures.find((c) => c.peer === selected) ?? null;

	return (
		<div className="flex min-h-0 flex-1 flex-col gap-4">
			<div>
				<h1 className="font-semibold text-2xl tracking-tight">Capture</h1>
				<p className="text-muted-foreground text-sm">
					Throwaway addresses that accept any call and show it here, live: inspect a webhook, a
					redirect, or a client without exposing a real service. Endpoints expire on their own; nothing
					is stored.
				</p>
			</div>

			{loaded && captures.length === 0 ? (
				<FirstEndpoint creating={creating} onCreate={create} />
			) : (
				<div className="flex min-h-0 flex-1 gap-4">
					<div className="flex w-56 shrink-0 flex-col gap-2">
						<Button disabled={creating} size="sm" variant="outline" onClick={create}>
							<Plus className="h-3.5 w-3.5" /> New endpoint
						</Button>
						<div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-auto">
							{captures.map((c) => (
								<EndpointItem
									key={c.peer}
									capture={c}
									onDelete={() => remove(c.peer)}
									onSelect={() => setSelected(c.peer)}
									selected={c.peer === selected}
								/>
							))}
						</div>
					</div>

					{current && (
						<div className="flex min-h-0 flex-1 flex-col gap-3">
							<CopyField label="Send anything to it" value={callHint(current.peer, config)} />
							<div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
								<TrafficPanel
									config={config}
									emptyHint="send the command above, or point a webhook at this endpoint"
									peer={current.peer}
								/>
							</div>
						</div>
					)}
				</div>
			)}
		</div>
	);
}

function EndpointItem({
	capture,
	selected,
	onSelect,
	onDelete,
}: {
	capture: Capture;
	selected: boolean;
	onSelect: () => void;
	onDelete: () => void;
}) {
	return (
		<div
			className={`group flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 transition-colors ${
				selected ? "border-ring bg-accent text-accent-foreground" : "hover:bg-accent/50"
			}`}
			onClick={onSelect}
			onKeyDown={(e) => e.key === "Enter" && onSelect()}
			role="button"
			tabIndex={0}
		>
			<div className="min-w-0 flex-1">
				<div className="truncate font-mono text-sm" title={capture.peer}>
					{capture.peer}
				</div>
				<div
					className="text-muted-foreground text-xs"
					title={`expires ${new Date(capture.expiresAt).toLocaleString()}`}
				>
					{formatExpiresIn(capture.expiresAt)}
				</div>
			</div>
			<Button
				className="h-6 w-6 shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
				size="icon"
				title="delete this endpoint"
				variant="ghost"
				onClick={(e) => {
					e.stopPropagation();
					onDelete();
				}}
			>
				<Trash2 className="h-3.5 w-3.5" />
			</Button>
		</div>
	);
}

// FirstEndpoint is the empty state before any endpoint exists.
function FirstEndpoint({ creating, onCreate }: { creating: boolean; onCreate: () => void }) {
	return (
		<div className="flex flex-1 flex-col items-center justify-center gap-4 rounded-lg border border-dashed py-20">
			<Inbox className="h-8 w-8 text-muted-foreground" />
			<div className="max-w-md text-center">
				<div className="font-medium">No capture endpoints yet</div>
				<p className="mt-1 text-muted-foreground text-sm">
					Create one and you get an address that answers anything with a small JSON receipt. Give it
					to a webhook sender, an OAuth redirect, or a teammate's curl: every request lands here,
					headers and body included, as it happens.
				</p>
			</div>
			<Button disabled={creating} onClick={onCreate}>
				<Plus className="h-4 w-4" /> Create an endpoint
			</Button>
		</div>
	);
}

// callHint is the simplest command that reaches the endpoint: its own
// hostname under subdomain routing, else the proxy with the header.
function callHint(peer: string, config: HubConfig): string {
	const subdomain = peerURL(peer, config.proxyDomain);
	if (subdomain) return `curl ${subdomain}`;

	const host = window.location.hostname || "127.0.0.1";
	const base = config.externalURL.replace(/\/$/, "") || `http://${host}:${config.proxyPort}`;

	return `curl -H '${config.routeHeader}: ${peer}' ${base}/`;
}

function formatExpiresIn(expiresAt: string) {
	const s = Math.max(0, Math.floor((new Date(expiresAt).getTime() - Date.now()) / 1000));
	const h = Math.floor(s / 3600);
	const m = Math.floor((s % 3600) / 60);
	if (h > 0) return `expires in ${h}h ${m}m`;
	if (m > 0) return `expires in ${m}m`;
	return `expires in ${s}s`;
}
