import { Radio, X } from "lucide-react";
import { useEffect } from "react";

import { Button } from "@/components/ui/button";
import { type LiveRequest, useLiveRequests } from "@/lib/use-request-stream";

// TrafficModal is one peer's live traffic: the requests the hub
// carried to it, newest first, as the responses complete. It is opened
// from that peer's row, which is what keeps a busy fleet readable —
// the hub streams this peer's requests and nobody else's.
//
// It is a window, not a history. The hub stores nothing; it holds a
// handful of recent requests in memory so the panel is not blank when
// it opens, and this list is gone when the modal closes.
export function TrafficModal({ peer, onClose }: { peer: string; onClose: () => void }) {
	const { live, requests, supported } = useLiveRequests(peer);

	useEffect(() => {
		const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	}, [onClose]);

	return (
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
			onClick={onClose}
			onKeyDown={() => {}}
			role="presentation"
		>
			<div
				className="flex max-h-[85vh] w-full max-w-3xl flex-col rounded-lg border bg-background shadow-lg"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={() => {}}
				role="dialog"
				aria-modal="true"
			>
				<div className="flex items-start justify-between border-b px-5 py-4">
					<div>
						<h2 className="flex items-center gap-2 font-semibold">
							<Radio className={`h-4 w-4 ${live ? "text-emerald-500" : "text-muted-foreground"}`} />
							Traffic <span className="font-mono text-sm">{peer}</span>
						</h2>
						<p className="mt-1 text-muted-foreground text-sm">
							Requests the hub carried to this peer, live. Nothing is stored: closing this loses them.
						</p>
					</div>
					<Button size="icon" variant="ghost" className="-mr-2 h-7 w-7 shrink-0" onClick={onClose}>
						<X className="h-4 w-4" />
					</Button>
				</div>

				<div className="overflow-auto px-5 py-4">
					{!supported ? (
						<div className="flex items-center justify-center rounded-lg border border-dashed py-10 text-muted-foreground text-sm">
							this hub does not report proxied requests
						</div>
					) : requests.length === 0 ? (
						<div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed py-12 text-muted-foreground text-sm">
							<span>waiting for traffic</span>
							<span className="text-xs">requests to this peer show up here as they happen</span>
						</div>
					) : (
						<ul className="flex flex-col gap-1 font-mono text-sm">
							{requests.map((r) => (
								<RequestRow key={r.id} request={r} />
							))}
						</ul>
					)}
				</div>

				<div className="border-t px-5 py-3 text-muted-foreground text-xs">
					Durations are measured at the hub, so they include the tunnel hop. The peer logs the same
					requests without it.
				</div>
			</div>
		</div>
	);
}

// RequestRow is one request, in the same columns the peer logs: when,
// what was asked, what came back, how long it took.
function RequestRow({ request }: { request: LiveRequest }) {
	return (
		<li className="flex items-baseline gap-3 border-border/40 border-b py-0.5 last:border-0">
			<span className="shrink-0 text-muted-foreground text-xs">
				{new Date(request.at).toLocaleTimeString()}
			</span>
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
