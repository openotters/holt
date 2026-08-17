import { ChevronDown, ChevronRight, Radio, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { type LiveRequest, useLiveRequests } from "@/lib/use-request-stream";

// The columns that can be sorted on. Time descending is the live feed;
// any other order is a view over the window, still updating underneath.
type SortKey = "at" | "method" | "path" | "status" | "tookMs";

// TrafficModal is one peer's live traffic: the requests the hub
// carried to it, as a table you can filter, sort, and open a row of
// for everything the hub knows about that request. It is opened from
// that peer's row, which is what keeps a busy fleet readable — the hub
// streams this peer's requests and nobody else's.
//
// It is a window, not a history. The hub stores nothing; it holds a
// bounded number of recent requests in memory (--traffic-buffer) so
// the table is not blank when it opens, and closing this loses the
// rest.
export function TrafficModal({ peer, onClose }: { peer: string; onClose: () => void }) {
	const { live, requests, supported } = useLiveRequests(peer);
	const [filter, setFilter] = useState("");
	const [sort, setSort] = useState<SortKey>("at");
	const [asc, setAsc] = useState(false);
	const [open, setOpen] = useState<number | null>(null);

	useEffect(() => {
		const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	}, [onClose]);

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
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
			onClick={onClose}
			onKeyDown={() => {}}
			role="presentation"
		>
			<div
				className="flex max-h-[85vh] w-full max-w-4xl flex-col rounded-lg border bg-background shadow-lg"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={() => {}}
				role="dialog"
				aria-modal="true"
			>
				<div className="flex items-start justify-between gap-3 border-b px-5 py-4">
					<div>
						<h2 className="flex items-center gap-2 font-semibold">
							<Radio className={`h-4 w-4 ${live ? "text-emerald-500" : "text-muted-foreground"}`} />
							Traffic <span className="font-mono text-sm">{peer}</span>
							{requests.length > 0 && (
								<span className="font-normal text-muted-foreground text-sm">
									{shown.length === requests.length
										? `${requests.length}`
										: `${shown.length} of ${requests.length}`}
								</span>
							)}
						</h2>
						<p className="mt-1 text-muted-foreground text-sm">
							Requests the hub carried to this peer, live. Click a row for the details. Nothing is
							stored: closing this loses them.
						</p>
					</div>
					<div className="flex shrink-0 items-center gap-2">
						<input
							className="h-8 w-44 rounded-md border bg-background px-2.5 text-sm"
							onChange={(e) => setFilter(e.target.value)}
							placeholder="filter path, method, 5xx"
							value={filter}
						/>
						<Button size="icon" variant="ghost" className="-mr-2 h-7 w-7" onClick={onClose}>
							<X className="h-4 w-4" />
						</Button>
					</div>
				</div>

				<div className="overflow-auto">
					{!supported ? (
						<Empty>this hub does not report proxied requests</Empty>
					) : requests.length === 0 ? (
						<Empty hint="requests to this peer show up here as they happen">waiting for traffic</Empty>
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
										expanded={open === r.id}
										onToggle={() => setOpen(open === r.id ? null : r.id)}
										request={r}
									/>
								))}
							</TableBody>
						</Table>
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

// Row is one request, with its details a click away: the columns carry
// what you scan for, the expansion carries everything else the hub
// knows (which is metadata, never a body).
function Row({
	request,
	expanded,
	onToggle,
}: {
	request: LiveRequest;
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
						<dl className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-1 border-border/60 border-l-2 bg-muted/30 px-4 py-3 text-sm">
							<Detail label="when">{new Date(request.at).toLocaleString()}</Detail>
							<Detail label="peer">{request.peer || "—"}</Detail>
							<Detail label="host">{request.host || "—"}</Detail>
							<Detail label="path">{request.path}</Detail>
							{request.query && <Detail label="query">{request.query}</Detail>}
							<Detail label="protocol">{request.proto || "—"}</Detail>
							<Detail label="client">{request.remoteAddr || "—"}</Detail>
							<Detail label="user agent">{request.userAgent || "—"}</Detail>
							<Detail label="request size">{formatBytes(request.requestBytes)}</Detail>
							<Detail label="response size">{formatBytes(request.responseBytes)}</Detail>
							<Detail label="status">{request.status === 0 ? "no response" : request.status}</Detail>
							<Detail label="took">{formatTook(request.tookMs)}</Detail>
						</dl>
					</TableCell>
				</TableRow>
			)}
		</>
	);
}

// Detail is one label/value pair in an expanded row.
function Detail({ label, children }: { label: string; children: React.ReactNode }) {
	return (
		<>
			<dt className="text-muted-foreground text-xs">{label}</dt>
			<dd className="break-all font-mono text-xs">{children}</dd>
		</>
	);
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

// formatBytes renders a size, and says so when there was none to
// declare (a streamed body, or no body at all).
function formatBytes(bytes: number): string {
	if (bytes < 0) return "unknown";
	if (bytes === 0) return "0 B";
	if (bytes < 1024) return `${bytes} B`;
	if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} kB`;
	return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
