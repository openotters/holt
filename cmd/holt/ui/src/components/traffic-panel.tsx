import { Check, ChevronDown, ChevronRight, Copy, Radio, Terminal } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { type JsonValue, JsonView } from "@/components/json-view";
import { Button } from "@/components/ui/button";
import { type HubConfig, curlFor, requestFormats } from "@/lib/reach";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { type CapturedBody, type LiveRequest, useLiveRequests } from "@/lib/use-request-stream";

// The columns that can be sorted on. Time descending is the live feed;
// any other order is a view over the window, still updating underneath.
type SortKey = "at" | "method" | "path" | "status" | "tookMs";

// TrafficPanel is one peer's live traffic: filterable, sortable, a row
// click away from everything the hub knows about a request. The traffic
// modal wraps it in a dialog; the capture page lays it beside the list.
// It is a window, not a history — unmounting loses it.
export function TrafficPanel({
	peer,
	config,
	emptyHint = "requests to this peer show up here as they happen",
}: {
	peer: string;
	config: HubConfig;
	emptyHint?: string;
}) {
	const { live, requests, supported } = useLiveRequests(peer);
	const [filter, setFilter] = useState("");
	const [sort, setSort] = useState<SortKey>("at");
	const [asc, setAsc] = useState(false);
	const [open, setOpen] = useState<number | null>(null);

	// One box for everything: a path fragment, a method, a status code
	// or its class ("5" matches every 5xx), so filtering is one thing
	// to learn rather than four controls to combine.
	const shown = useMemo(() => {
		const q = filter.trim().toLowerCase();
		const matched = q
			? requests.filter(
					(r) =>
						r.path.toLowerCase().includes(q) ||
						r.query.toLowerCase().includes(q) ||
						r.method.toLowerCase().startsWith(q) ||
						String(r.status).startsWith(q),
				)
			: requests;

		return [...matched].sort((a, b) => compare(a, b, sort) * (asc ? 1 : -1));
	}, [asc, filter, requests, sort]);

	function sortBy(key: SortKey) {
		if (key === sort) {
			setAsc(!asc);
			return;
		}
		setSort(key);
		// Time reads newest-first, everything else biggest-first, which
		// is what you want the moment you click it (slowest, worst).
		setAsc(false);
	}

	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<div className="flex shrink-0 items-center gap-2 border-b px-4 py-2.5">
				<Radio className={`h-3.5 w-3.5 ${live ? "animate-pulse text-emerald-500" : "text-muted-foreground"}`} />
				<span className="text-muted-foreground text-xs">
					{live ? "live" : "reconnecting"}
					{requests.length > 0 &&
						(shown.length === requests.length
							? ` · ${requests.length} request${requests.length === 1 ? "" : "s"}`
							: ` · ${shown.length} of ${requests.length}`)}
				</span>
				<input
					className="ml-auto h-8 w-44 rounded-md border bg-background px-2.5 text-sm"
					onChange={(e) => setFilter(e.target.value)}
					placeholder="filter path, method, 5xx"
					value={filter}
				/>
			</div>

			<div className="min-h-0 flex-1 overflow-auto">
				{!supported ? (
					<Empty>this hub does not report proxied requests</Empty>
				) : requests.length === 0 ? (
					<Empty hint={emptyHint}>waiting for traffic</Empty>
				) : shown.length === 0 ? (
					<Empty hint="clear the filter to see the rest">nothing matches</Empty>
				) : (
					// Edge to edge: the rows are the content, and a
					// fast-moving list reads better without a frame
					// around it.
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead className="w-6 pl-4" />
								<SortHead active={sort} asc={asc} onSort={sortBy} sortKey="at">
									Time
								</SortHead>
								<SortHead active={sort} asc={asc} onSort={sortBy} sortKey="method">
									Method
								</SortHead>
								<SortHead active={sort} asc={asc} onSort={sortBy} sortKey="path">
									Path
								</SortHead>
								<SortHead active={sort} asc={asc} className="text-right" onSort={sortBy} sortKey="status">
									Status
								</SortHead>
								<SortHead active={sort} asc={asc} className="pr-4 text-right" onSort={sortBy} sortKey="tookMs">
									Took
								</SortHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{shown.map((r) => (
								<Row
									key={r.id}
									config={config}
									expanded={open === r.id}
									onToggle={() => setOpen(open === r.id ? null : r.id)}
									peer={peer}
									request={r}
								/>
							))}
						</TableBody>
					</Table>
				)}
			</div>
		</div>
	);
}

// Row is one request, with its details a click away: the columns carry
// what you scan for, the expansion carries everything else the hub
// knows (which is metadata, never a body).
function Row({
	request,
	peer,
	config,
	expanded,
	onToggle,
}: {
	request: LiveRequest;
	peer: string;
	config: HubConfig;
	expanded: boolean;
	onToggle: () => void;
}) {
	const Chevron = expanded ? ChevronDown : ChevronRight;

	return (
		<>
			<TableRow className="cursor-pointer" onClick={onToggle}>
				<TableCell className="pr-0 pl-4 text-muted-foreground">
					<Chevron className="h-3.5 w-3.5" />
				</TableCell>
				<TableCell className="font-mono text-muted-foreground text-xs">
					{new Date(request.at).toLocaleTimeString()}
				</TableCell>
				<TableCell className="font-medium font-mono text-sky-500">{request.method}</TableCell>
				<TableCell className="max-w-md truncate font-mono" title={request.path}>
					{request.path}
					{request.query && <span className="text-muted-foreground">?{request.query}</span>}
				</TableCell>
				<TableCell className={`text-right font-mono ${statusColor(request.status)}`}>
					{request.status === 0 ? "---" : request.status}
				</TableCell>
				<TableCell className="pr-4 text-right font-mono text-muted-foreground text-xs">
					{formatTook(request.tookMs)}
				</TableCell>
			</TableRow>
			{expanded && (
				<TableRow className="hover:bg-transparent">
					<TableCell className="p-0" colSpan={6}>
						<Details config={config} peer={peer} request={request} />
					</TableCell>
				</TableRow>
			)}
		</>
	);
}

// Details is the whole request as a structured entry: everything the
// hub knows, foldable, with two things to take away — the entry as
// JSON (for an issue, a paste, a grep) and the request as curl (to ask
// again).
function Details({ request, peer, config }: { request: LiveRequest; peer: string; config: HubConfig }) {
	const entry = asEntry(request);

	return (
		// w-0 min-w-full is what keeps a long entry inside the panel: the
		// div takes its width from the table rather than from its
		// content, so the lines scroll here instead of widening every
		// row above them. The frame does not scroll, which is what keeps
		// the buttons where they were put.
		<div className="relative w-0 min-w-full border-border/60 border-l-2 bg-muted/30">
			<div className="absolute top-2 right-3 z-10 flex items-center gap-1 rounded-md bg-muted/80 backdrop-blur">
				<CopyAsMenu config={config} peer={peer} request={request} />
				<CopyButton
					icon={Copy}
					label="copy"
					title="copy this entry as JSON"
					value={JSON.stringify(entry, null, 2)}
				/>
			</div>
			<div className="overflow-x-auto py-3">
				<JsonView value={entry} />
			</div>
		</div>
	);
}

// CopyButton puts something on the clipboard and says it did.
function CopyButton({
	value,
	label,
	title,
	icon: Icon,
}: {
	value: string;
	label: string;
	title: string;
	icon: typeof Copy;
}) {
	const [copied, setCopied] = useState(false);

	async function copy() {
		await navigator.clipboard.writeText(value);
		setCopied(true);
		setTimeout(() => setCopied(false), 1200);
	}

	return (
		<Button className="h-7 gap-1.5 text-xs" onClick={copy} size="sm" title={title} variant="ghost">
			{copied ? <Check className="h-3 w-3" /> : <Icon className="h-3 w-3" />}
			{copied ? "copied" : label}
		</Button>
	);
}

// CopyAsMenu is the curl button with the other formats behind a
// chevron: one click for the common case, a menu for a Windows box, a
// browser console, or a machine with only wget.
function CopyAsMenu({
	request,
	peer,
	config,
}: {
	request: LiveRequest;
	peer: string;
	config: HubConfig;
}) {
	const [open, setOpen] = useState(false);
	const [copied, setCopied] = useState("");
	const root = useRef<HTMLDivElement>(null);

	useEffect(() => {
		if (!open) return;
		const onDown = (e: MouseEvent) => {
			if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
		};
		const onKey = (e: KeyboardEvent) => {
			// Stops at the menu: closing it should not also close the
			// panel behind it.
			if (e.key === "Escape") {
				e.stopPropagation();
				setOpen(false);
			}
		};
		document.addEventListener("mousedown", onDown);
		document.addEventListener("keydown", onKey, true);
		return () => {
			document.removeEventListener("mousedown", onDown);
			document.removeEventListener("keydown", onKey, true);
		};
	}, [open]);

	async function copy(label: string, value: string) {
		await navigator.clipboard.writeText(value);
		setCopied(label);
		setOpen(false);
		setTimeout(() => setCopied(""), 1200);
	}

	return (
		<div className="relative flex items-center" ref={root}>
			<Button
				className="h-7 gap-1.5 rounded-r-none pr-2 text-xs"
				onClick={() => copy("curl", curlFor(request, peer, config))}
				size="sm"
				title="copy a curl that replays this request through the hub"
				variant="ghost"
			>
				{copied ? <Check className="h-3 w-3" /> : <Terminal className="h-3 w-3" />}
				{copied || "curl"}
			</Button>
			<Button
				aria-expanded={open}
				aria-haspopup="menu"
				className="h-7 rounded-l-none border-border/60 border-l px-1 text-xs"
				onClick={() => setOpen((o) => !o)}
				size="sm"
				title="copy this request another way"
				variant="ghost"
			>
				<ChevronDown className={`h-3 w-3 transition-transform ${open ? "rotate-180" : ""}`} />
			</Button>
			{open && (
				<div className="absolute top-8 right-0 z-10 min-w-52 overflow-hidden rounded-md border bg-popover py-1 shadow-lg">
					{requestFormats.map((f) => (
						<button
							className="block w-full px-3 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
							key={f.label}
							onClick={() => copy(f.label, f.render(request, peer, config))}
							type="button"
						>
							{f.label}
						</button>
					))}
				</div>
			)}
		</div>
	);
}

// asEntry shapes a request the way a log entry reads: what it was at
// the top, the HTTP details grouped under one key, then each half's
// headers and body. Absent values are null rather than missing, so two
// entries line up when read one after the other.
function asEntry(r: LiveRequest): JsonValue {
	const entry: Record<string, JsonValue> = {
		timestamp: new Date(r.at).toISOString(),
		peer: r.peer || null,
		latency: formatTook(r.tookMs),
		httpRequest: {
			method: r.method,
			path: r.path,
			query: r.query || null,
			host: r.host || null,
			status: r.status,
			protocol: r.proto || null,
			remoteIp: r.remoteAddr || null,
			userAgent: r.userAgent || null,
			requestSize: r.requestBytes < 0 ? null : r.requestBytes,
			responseSize: r.responseBytes,
			latencyMs: Number(r.tookMs.toFixed(3)),
		},
	};

	// Only present when the hub captures payloads: a request that
	// carried nothing says so with an absent key rather than an empty
	// one, and a hub with capture off shows none of these at all.
	if (r.requestHeaders && Object.keys(r.requestHeaders).length > 0) {
		entry.requestHeaders = r.requestHeaders;
	}

	if (r.requestBody) entry.requestBody = asBodyValue(r.requestBody);

	if (r.responseHeaders && Object.keys(r.responseHeaders).length > 0) {
		entry.responseHeaders = r.responseHeaders;
	}

	if (r.responseBody) entry.responseBody = asBodyValue(r.responseBody);

	return entry;
}

// asBodyValue renders a captured payload: the content when there is
// some, otherwise why there is not. A truncated body says so next to
// the size it was cut from, so nobody reads a prefix as the whole.
function asBodyValue(body: CapturedBody): JsonValue {
	if (body.skipped === "disabled") {
		return { bytes: body.size, captured: false, reason: "capture is off on this hub" };
	}

	if (body.skipped === "binary") {
		return { bytes: body.size, captured: false, reason: "not a text content type" };
	}

	if (body.truncated) {
		return { bytes: body.size, truncated: true, content: body.content };
	}

	return { bytes: body.size, content: body.content };
}

// SortHead is a column header that sorts, and shows which way.
function SortHead({
	children,
	sortKey,
	active,
	asc,
	onSort,
	className = "",
}: {
	children: React.ReactNode;
	sortKey: SortKey;
	active: SortKey;
	asc: boolean;
	onSort: (key: SortKey) => void;
	className?: string;
}) {
	const on = active === sortKey;

	return (
		<TableHead className={className}>
			<button
				className={`inline-flex items-center gap-1 ${on ? "text-foreground" : "hover:text-foreground"}`}
				onClick={() => onSort(sortKey)}
				type="button"
			>
				{children}
				<span className="text-xs">{on ? (asc ? "↑" : "↓") : ""}</span>
			</button>
		</TableHead>
	);
}

// Empty is the placeholder the table is replaced by when there is
// nothing to show, with an optional line saying what to do about it.
function Empty({ children, hint }: { children: React.ReactNode; hint?: string }) {
	return (
		<div className="m-5 flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed py-12 text-muted-foreground text-sm">
			<span>{children}</span>
			{hint && <span className="text-xs">{hint}</span>}
		</div>
	);
}

// compare orders two requests on one column; paths and methods sort as
// text, everything else as a number.
function compare(a: LiveRequest, b: LiveRequest, key: SortKey): number {
	switch (key) {
		case "method":
			return a.method.localeCompare(b.method);
		case "path":
			return a.path.localeCompare(b.path);
		case "status":
			return a.status - b.status;
		case "tookMs":
			return a.tookMs - b.tookMs;
		default:
			return a.at - b.at;
	}
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
