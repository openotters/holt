import { Radio } from "lucide-react";
import { useMemo, useState } from "react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { type LiveRequest, useLiveRequests } from "@/lib/use-request-stream";

// RequestsCard is the hub's live view of what it carried: one line per
// request, newest first, as the responses complete. It is a window,
// not a history — the hub stores nothing, and this list is capped and
// lost on reload, which is why the card says so rather than pretending
// to be a log.
export function RequestsCard() {
	const { live, requests, supported } = useLiveRequests();
	const [peer, setPeer] = useState<string>("");

	// The peers seen in the current window, so the filter offers what
	// is actually there rather than every peer ever attached.
	const peers = useMemo(() => {
		return [...new Set(requests.map((r) => r.peer).filter(Boolean))].sort();
	}, [requests]);

	const shown = peer ? requests.filter((r) => r.peer === peer) : requests;

	return (
		<Card>
			<CardHeader>
				<div className="flex items-center justify-between gap-3">
					<CardTitle className="flex items-center gap-2">
						<Radio className={`h-4 w-4 ${live ? "text-emerald-500" : "text-muted-foreground"}`} />
						Traffic
						<span className="font-normal text-muted-foreground text-sm">
							{supported ? "live, not stored" : "unavailable"}
						</span>
					</CardTitle>
					{peers.length > 1 && (
						<select
							className="h-8 rounded-md border bg-background px-2 text-sm"
							onChange={(e) => setPeer(e.target.value)}
							value={peer}
						>
							<option value="">all peers</option>
							{peers.map((p) => (
								<option key={p} value={p}>
									{p}
								</option>
							))}
						</select>
					)}
				</div>
			</CardHeader>
			<CardContent>
				{!supported ? (
					<div className="flex items-center justify-center rounded-lg border border-dashed py-8 text-muted-foreground text-sm">
						this hub does not report proxied requests
					</div>
				) : shown.length === 0 ? (
					<div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed py-10 text-muted-foreground text-sm">
						<span>waiting for traffic</span>
						<span className="text-xs">requests through the proxy show up here as they happen</span>
					</div>
				) : (
					<ul className="flex max-h-96 flex-col gap-1 overflow-auto font-mono text-sm">
						{shown.map((r) => (
							<RequestRow key={r.id} request={r} showPeer={!peer} />
						))}
					</ul>
				)}
			</CardContent>
		</Card>
	);
}

// RequestRow is one request, in the same columns the CLI prints: when,
// which peer, what was asked, what came back, how long it took.
function RequestRow({ request, showPeer }: { request: LiveRequest; showPeer: boolean }) {
	return (
		<li className="flex items-baseline gap-3 border-border/40 border-b py-0.5 last:border-0">
			<span className="shrink-0 text-muted-foreground text-xs">
				{new Date(request.at).toLocaleTimeString()}
			</span>
			{showPeer && <span className="w-28 shrink-0 truncate text-violet-400">{request.peer}</span>}
			<span className="w-12 shrink-0 font-medium text-sky-500">{request.method}</span>
			<span className="flex-1 truncate" title={request.path}>
				{request.path}
			</span>
			<span className={`w-9 shrink-0 text-right ${statusColor(request.status)}`}>
				{request.status === 0 ? "---" : request.status}
			</span>
			<span className="w-14 shrink-0 text-right text-muted-foreground text-xs">
				{formatTook(request.tookMs)}
			</span>
		</li>
	);
}

// statusColor reads by class, so a fast-moving list is scannable
// before the digits are: 4xx is the caller's problem, 5xx (and no
// response at all) is the hub's.
function statusColor(status: number): string {
	if (status === 0 || status >= 500) return "text-red-500";
	if (status >= 400) return "text-amber-500";
	return "text-emerald-500";
}

// formatTook renders in units a human reads at a glance.
function formatTook(ms: number): string {
	if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
	if (ms >= 1) return `${Math.round(ms)}ms`;
	return `${Math.round(ms * 1000)}µs`;
}
