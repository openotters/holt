import { useQuery } from "@connectrpc/connect-query";
import { Check, ChevronDown, Copy, Radio } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { info } from "@/gen/v1/admin-Admin_connectquery";

// StatusMenu is the connection item in the top navbar: a real menu
// (click to open, stays open, text selectable), not a hover tooltip.
// It is the "about this hub" card: identity (endpoint, version), the
// two URLs an operator hands out (tunnel, proxy), and what a call
// needs (route header, token ttl).
export function StatusMenu({
	error,
	proxyURL,
	tunnelURL,
}: {
	error: unknown;
	proxyURL: string;
	tunnelURL: string;
}) {
	const [open, setOpen] = useState(false);
	const root = useRef<HTMLDivElement>(null);

	// Build/version and token ttl come from the Admin Info RPC; the
	// rows simply hide while it has not answered (first paint, or an
	// unreachable hub).
	const { data: hubInfo } = useQuery(info, {});

	useEffect(() => {
		if (!open) return;
		const onDown = (e: MouseEvent) => {
			if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") setOpen(false);
		};
		document.addEventListener("mousedown", onDown);
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("mousedown", onDown);
			document.removeEventListener("keydown", onKey);
		};
	}, [open]);

	const row = "flex items-baseline justify-between gap-6";
	const key = "whitespace-nowrap text-muted-foreground";

	return (
		<div className="relative" ref={root}>
			<button
				aria-expanded={open}
				aria-haspopup="menu"
				className="inline-flex items-center gap-2 rounded-md border border-transparent px-3 py-1.5 text-muted-foreground text-sm transition-colors hover:border-border hover:bg-accent hover:text-accent-foreground data-[open=true]:border-border data-[open=true]:bg-accent"
				data-open={open}
				onClick={() => setOpen((o) => !o)}
				type="button"
			>
				<Radio className={`h-4 w-4 ${error ? "text-red-500" : "text-emerald-500"}`} />
				{error ? "unreachable" : "connected"}
				<ChevronDown className={`h-3.5 w-3.5 transition-transform ${open ? "rotate-180" : ""}`} />
			</button>

			{open && (
				<div className="absolute top-full right-0 z-50 mt-2 w-96 select-text rounded-md border bg-popover p-4 text-popover-foreground text-sm shadow-md">
					<div className="flex flex-col gap-2">
						<div className={row}>
							<span className={key}>status</span>
							<span className={error ? "text-red-500" : "text-emerald-500"}>
								{error ? "unreachable" : "connected"}
							</span>
						</div>
						<div className={row}>
							<span className={key}>endpoint</span>
							<span className="font-mono">{window.location.host}</span>
						</div>
						{hubInfo && (
							<div className={row}>
								<span className={key}>version</span>
								<span className="font-mono">
									{hubInfo.version}
									{hubInfo.commit ? ` (${hubInfo.commit.slice(0, 7)})` : ""}
								</span>
							</div>
						)}
						<div className={row}>
							<span className={key}>route header</span>
							<span className="font-mono">{hubInfo?.routeHeader || "x-tunnel-peer"}</span>
						</div>
						{hubInfo && hubInfo.tokenTtlSeconds > 0n && (
							<div className={row}>
								<span className={key}>token ttl</span>
								<span className="font-mono">{formatTTL(hubInfo.tokenTtlSeconds)}</span>
							</div>
						)}

						<UrlChip label="tunnel url" value={tunnelURL} hint="peers dial this (stamped into tokens)" />
						<UrlChip label="proxy url" value={proxyURL} hint="calls land here, routed by the header" />

						{error instanceof Error && (
							<div className="mt-1 border-t border-dashed pt-2 text-red-500">{error.message}</div>
						)}
					</div>
				</div>
			)}
		</div>
	);
}

// UrlChip is a labelled, copyable URL on its own full-width line, so
// long hostnames never fight the label for horizontal space.
function UrlChip({ label, value, hint }: { label: string; value: string; hint: string }) {
	const [copied, setCopied] = useState(false);

	if (!value) return null;

	async function copy() {
		await navigator.clipboard.writeText(value);
		setCopied(true);
		setTimeout(() => setCopied(false), 1200);
	}

	return (
		<div className="flex flex-col gap-1 pt-1">
			<span className="text-muted-foreground text-xs">
				{label}
				<span className="text-muted-foreground/60"> · {hint}</span>
			</span>
			<div className="flex items-center gap-2 rounded-md border bg-muted/50 py-1 pr-1 pl-3 font-mono text-xs">
				<code className="flex-1 break-all">{value}</code>
				<button
					className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
					onClick={copy}
					title={`Copy ${label}`}
					type="button"
				>
					{copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
				</button>
			</div>
		</div>
	);
}

// formatTTL renders seconds as compact hours/minutes ("24h", "1h30m").
function formatTTL(seconds: bigint) {
	const s = Number(seconds);
	const h = Math.floor(s / 3600);
	const m = Math.floor((s % 3600) / 60);
	if (h && m) return `${h}h${m}m`;
	if (h) return `${h}h`;
	if (m) return `${m}m`;
	return `${s}s`;
}
