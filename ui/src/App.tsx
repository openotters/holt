import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { Ban, Check, Copy, Github, RefreshCw, ShieldAlert, ShieldOff, Star, Terminal, X } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { Footer } from "@/components/footer";
import { StatusBadge } from "@/components/status-badge";
import { StatusMenu } from "@/components/status-menu";
import { useLiveTunnels } from "@/lib/use-tunnel-stream";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { blockPeer, listBlocked, listTunnels, stopTunnel, unblockPeer } from "@/gen/v1/admin-Admin_connectquery";

// Proper connect-query keys: the v2 key shape is ["connect-query",
// {...}], so hand-written ["Service", "Method"] arrays match nothing
// and invalidations silently no-op. cardinality undefined = filter
// matching both finite and infinite queries.
const BLOCKED_KEY = createConnectQueryKey({ cardinality: undefined, schema: listBlocked });

const REPO_URL = "https://github.com/openotters/holt";

// useHubConfig fetches the proxy port + routing header once, so the
// "call this peer" command points at the right address (the console is
// served from the admin port, not the proxy one).
function useHubConfig() {
	const [cfg, setCfg] = useState<{ routeHeader: string; proxyPort: string; externalURL: string }>({
		routeHeader: "x-tunnel-peer",
		proxyPort: "7002",
		externalURL: "",
	});
	useEffect(() => {
		fetch("/api/config")
			.then((r) => (r.ok ? r.json() : null))
			.then((c) => c && setCfg(c))
			.catch(() => {});
	}, []);
	return cfg;
}

export function App() {
	const queryClient = useQueryClient();
	const config = useHubConfig();
	const [callPeer, setCallPeer] = useState<string | null>(null);

	const { data, error, isLoading, dataUpdatedAt } = useQuery(listTunnels, {});

	// While the stream is live the tunnel list is maintained client
	// side from its events (zero ListTunnels per change); the query is
	// the initial render and the fallback when the stream is down.
	const stream = useLiveTunnels();
	const live = stream.live;
	const tunnels = live ? stream.tunnels : (data?.tunnels ?? []);

	const invalidateBlocked = () => queryClient.invalidateQueries({ queryKey: BLOCKED_KEY });

	// Kill/block do not invalidate the tunnels list themselves: the
	// WatchTunnels stream reports the detach and triggers the single
	// refetch. Only the blocked list (not streamed) is invalidated.
	const kill = useMutation(stopTunnel, {
		onError: (e) => toast.error("Kill failed", { description: e.message }),
	});
	const block = useMutation(blockPeer, {
		onSuccess: (_r, req) => {
			toast.success(`Blocked ${req.peer}`);
			invalidateBlocked();
		},
		onError: (e) => toast.error("Block failed", { description: e.message }),
	});
	const unblock = useMutation(unblockPeer, {
		onSuccess: (_r, req) => {
			toast.success(`Unblocked ${req.peer}`);
			invalidateBlocked();
		},
		onError: (e) => toast.error("Unblock failed", { description: e.message }),
	});

	return (
		<div className="flex min-h-screen flex-col font-sans antialiased">
			<header className="flex h-16 shrink-0 items-center gap-3 border-b border-dashed px-6">
				<span className="text-xl leading-none" aria-hidden="true">
					🌀
				</span>
				<span className="font-semibold tracking-tight">holt console</span>
				<div className="ml-auto flex items-center gap-1.5">
					<a
						className="inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-muted-foreground text-xs transition-colors hover:bg-accent hover:text-accent-foreground"
						href={`${REPO_URL}/stargazers`}
						rel="noreferrer"
						target="_blank"
						title="Star holt on GitHub"
					>
						<Star className="h-3.5 w-3.5" /> Star
					</a>
					<a
						className="inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
						href={REPO_URL}
						rel="noreferrer"
						target="_blank"
						title="holt on GitHub"
					>
						<Github className="h-4 w-4" />
					</a>
					<StatusMenu error={error} live={live} updatedAt={live ? stream.lastEventAt : dataUpdatedAt} />
				</div>
			</header>

			<main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-6">
				<div>
					<h1 className="font-semibold text-2xl tracking-tight">Tunnels</h1>
					<p className="text-muted-foreground text-sm">
						Live reverse tunnels attached to this hub. Kill disconnects a peer (it may reconnect); block
						also bans its peer id so no token for it works until unblocked.
					</p>
				</div>

				<EnrollCard />

				<Card>
					<CardHeader>
						<CardTitle>
							Attached peers
							<span className="ml-2 font-normal text-muted-foreground text-sm">
								{tunnels.length > 0 ? `${tunnels.length}` : ""}
							</span>
						</CardTitle>
					</CardHeader>
					<CardContent>
						{error ? (
							<div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-sm">
								Could not reach the hub Admin API: {error.message}
							</div>
						) : isLoading ? (
							<p className="py-8 text-center text-muted-foreground text-sm">loading…</p>
						) : tunnels.length === 0 ? (
							<div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12 text-muted-foreground text-sm">
								no peers attached
							</div>
						) : (
							<Table>
								<TableHeader>
									<TableRow>
										<TableHead>Peer</TableHead>
										<TableHead>Status</TableHead>
										<TableHead>Version</TableHead>
										<TableHead>Attached</TableHead>
										<TableHead className="text-right">Actions</TableHead>
									</TableRow>
								</TableHeader>
								<TableBody>
									{tunnels.map((t) => (
										<TableRow key={t.peer}>
											<TableCell className="font-mono font-medium">{t.peer}</TableCell>
											<TableCell>
												<StatusBadge status="attached" />
											</TableCell>
											<TableCell className="text-muted-foreground">{t.peerVersion || "—"}</TableCell>
											<TableCell className="text-muted-foreground">
												{t.attachedAtUnix
													? new Date(Number(t.attachedAtUnix) * 1000).toLocaleTimeString()
													: "—"}
											</TableCell>
											<TableCell className="text-right">
												<div className="inline-flex items-center gap-1.5">
													<Button size="sm" variant="outline" onClick={() => setCallPeer(t.peer)}>
														<Terminal className="h-3.5 w-3.5" /> Call
													</Button>
													<Button
														size="sm"
														variant="outline"
														onClick={() => kill.mutate({ peer: t.peer })}
													>
														<X className="h-3.5 w-3.5" /> Kill
													</Button>
													<Button
														size="sm"
														variant="destructive"
														onClick={() => block.mutate({ peer: t.peer })}
													>
														<Ban className="h-3.5 w-3.5" /> Block
													</Button>
												</div>
											</TableCell>
										</TableRow>
									))}
								</TableBody>
							</Table>
						)}
					</CardContent>
				</Card>

				<BlockedCard onUnblock={(peer) => unblock.mutate({ peer })} />

				<DangerZone />
			</main>

			<Footer />

			{callPeer && (
				<CallPeerModal
					peer={callPeer}
					routeHeader={config.routeHeader}
					proxyPort={config.proxyPort}
					externalURL={config.externalURL}
					onClose={() => setCallPeer(null)}
				/>
			)}
		</div>
	);
}

// CallPeerModal shows the curl command that reaches a peer through the
// hub proxy. The host is the one you're viewing the console on, which
// is correct for loopback and single-host deployments; adjust it for
// anything fronted by a different proxy address.
function CallPeerModal({
	peer,
	routeHeader,
	proxyPort,
	externalURL,
	onClose,
}: {
	peer: string;
	routeHeader: string;
	proxyPort: string;
	externalURL: string;
	onClose: () => void;
}) {
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	}, [onClose]);

	const host = window.location.hostname || "127.0.0.1";
	const curl = `curl -H '${routeHeader}: ${peer}' http://${host}:${proxyPort}/`;
	const externalCurl = externalURL ? `curl -H '${routeHeader}: ${peer}' ${externalURL}/` : "";

	return (
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
			onClick={onClose}
			onKeyDown={() => {}}
			role="presentation"
		>
			<div
				className="w-full max-w-lg rounded-lg border bg-background p-5 shadow-lg"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={() => {}}
				role="dialog"
				aria-modal="true"
			>
				<div className="mb-3 flex items-center justify-between">
					<h2 className="flex items-center gap-2 font-semibold">
						<Terminal className="h-4 w-4" /> Call{" "}
						<span className="font-mono text-sm">{peer}</span>
					</h2>
					<Button size="icon" variant="ghost" className="h-7 w-7" onClick={onClose}>
						<X className="h-4 w-4" />
					</Button>
				</div>
				<p className="mb-3 text-muted-foreground text-sm">
					Requests to the hub proxy are routed to this peer by the{" "}
					<code className="font-mono text-xs">{routeHeader}</code> header. The peer serves whatever handler
					it attached with.
				</p>
				{externalCurl && (
					<div className="mb-3">
						<CopyField label="Through the public URL" value={externalCurl} multiline />
					</div>
				)}
				<CopyField
					label={externalCurl ? "From this host (loopback / in-cluster)" : "Reach the peer through the hub"}
					value={curl}
					multiline
				/>
			</div>
		</div>
	);
}

// EnrollCard mints a join token via the hub's /api/enroll endpoint and
// shows both the raw token (for any client) and the ready-to-run
// expose command (to tunnel a local endpoint).
function EnrollCard() {
	const [peer, setPeer] = useState("");
	const [result, setResult] = useState<{ token: string; command: string } | null>(null);

	async function enroll() {
		const name = peer.trim();
		if (!name) return;

		const res = await fetch("/api/enroll", {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({ peer: name }),
		});
		if (!res.ok) {
			toast.error("Enroll failed", { description: await res.text() });
			return;
		}
		setResult((await res.json()) as { token: string; command: string });
	}

	return (
		<Card>
			<CardHeader>
				<CardTitle>Add a peer</CardTitle>
			</CardHeader>
			<CardContent className="flex flex-col gap-3">
				<div className="flex gap-2">
					<input
						value={peer}
						onChange={(e) => setPeer(e.target.value)}
						onKeyDown={(e) => e.key === "Enter" && enroll()}
						placeholder="peer id, e.g. alice"
						className="h-9 flex-1 rounded-md border bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
					/>
					<Button onClick={enroll}>Generate token</Button>
				</div>

				{result && (
					<div className="flex flex-col gap-3">
						<CopyField
							label="Token — for any client (starter-client, your own)"
							value={result.token}
							multiline
						/>
						<CopyField
							label="Expose a local endpoint"
							value={result.command}
						/>
					</div>
				)}

				<p className="text-muted-foreground text-xs">
					The token carries a short-lived JWT and the hub's certificate to pin. Run the expose command on
					the machine you want to reach.
				</p>
			</CardContent>
		</Card>
	);
}

// CopyField shows a labelled, copyable mono value in the openotters
// command-chip style. With multiline, the value wraps across lines and
// scrolls if tall (for the long join token); the copy button always
// copies the clean single-line value.
function CopyField({ label, value, multiline = false }: { label: string; value: string; multiline?: boolean }) {
	const [copied, setCopied] = useState(false);

	async function copy() {
		await navigator.clipboard.writeText(value);
		setCopied(true);
		setTimeout(() => setCopied(false), 1200);
	}

	return (
		<div className="flex flex-col gap-1">
			<span className="text-muted-foreground text-xs">{label}</span>
			<div
				className={`flex gap-2 rounded-md border bg-muted/50 py-1 pr-1 pl-3 font-mono text-xs ${multiline ? "items-start" : "items-center"}`}
			>
				<code className={multiline ? "max-h-32 flex-1 overflow-auto whitespace-pre-wrap break-all py-1" : "truncate"}>
					{value}
				</code>
				<Button
					size="icon"
					variant="ghost"
					className={`h-6 w-6 shrink-0 ${multiline ? "sticky top-1" : ""}`}
					onClick={copy}
				>
					{copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
				</Button>
			</div>
		</div>
	);
}

// DangerZone holds destructive, hub-wide actions. Renewing the
// certificate invalidates every join token (peers pinned the old cert),
// so it is gated behind an inline two-step confirmation rather than a
// one-click button.
function DangerZone() {
	const [confirming, setConfirming] = useState(false);
	const [renewing, setRenewing] = useState(false);

	const renew = async () => {
		setRenewing(true);
		try {
			const res = await fetch("/api/renew", { method: "POST" });
			if (!res.ok) throw new Error(await res.text());
			const { closedTunnels = 0 } = (await res.json()) as { closedTunnels?: number };
			toast.success("Certificate renewed", {
				description:
					`Closed ${closedTunnels} tunnel${closedTunnels === 1 ? "" : "s"}. ` +
					"Existing join tokens are now invalid — re-enroll your peers.",
			});
		} catch (e) {
			toast.error("Renew failed", { description: e instanceof Error ? e.message : String(e) });
		} finally {
			setRenewing(false);
			setConfirming(false);
		}
	};

	return (
		<Card className="border-red-500/40">
			<CardHeader>
				<CardTitle className="flex items-center gap-2 text-red-500">
					<ShieldAlert className="h-4 w-4" /> Danger zone
				</CardTitle>
			</CardHeader>
			<CardContent>
				<div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
					<div>
						<div className="font-medium text-sm">Renew hub certificate</div>
						<p className="text-muted-foreground text-sm">
							Generates a fresh certificate and serves it immediately. Every existing join token pins the
							old certificate and stops working — peers must be re-enrolled.
						</p>
					</div>
					{confirming ? (
						<div className="flex shrink-0 items-center gap-2">
							<Button size="sm" variant="destructive" disabled={renewing} onClick={renew}>
								<RefreshCw className={`h-3.5 w-3.5 ${renewing ? "animate-spin" : ""}`} />
								{renewing ? "Renewing…" : "Confirm renew"}
							</Button>
							<Button size="sm" variant="secondary" disabled={renewing} onClick={() => setConfirming(false)}>
								Cancel
							</Button>
						</div>
					) : (
						<Button
							className="shrink-0"
							size="sm"
							variant="outline"
							onClick={() => setConfirming(true)}
						>
							<RefreshCw className="h-3.5 w-3.5" /> Renew certificate
						</Button>
					)}
				</div>
			</CardContent>
		</Card>
	);
}

// BlockedCard lists the currently-blocked peers with an unblock action
// each — blocked peers have no live tunnel, so they never appear in the
// tunnels table.
function BlockedCard({ onUnblock }: { onUnblock: (peer: string) => void }) {
	const { data } = useQuery(listBlocked, {});
	const blocked = data?.peers ?? [];

	return (
		<Card>
			<CardHeader>
				<CardTitle className="flex items-center gap-2">
					<ShieldOff className="h-4 w-4 text-muted-foreground" /> Blocked peers
					<span className="ml-1 font-normal text-muted-foreground text-sm">
						{blocked.length > 0 ? `${blocked.length}` : ""}
					</span>
				</CardTitle>
				<p className="text-muted-foreground text-sm">
					The ban is on the peer id, not one token: while blocked, every token for that id is refused, even
					a freshly enrolled one. Unblocking re-admits the peer, and tokens minted before the block that
					have not expired become valid again.
				</p>
			</CardHeader>
			<CardContent>
				{blocked.length === 0 ? (
					<div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-8 text-muted-foreground text-sm">
						no peers blocked
					</div>
				) : (
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Peer</TableHead>
								<TableHead>Status</TableHead>
								<TableHead>Blocked</TableHead>
								<TableHead className="text-right">Actions</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{blocked.map((b) => (
								<TableRow key={b.peer}>
									<TableCell className="font-mono font-medium">{b.peer}</TableCell>
									<TableCell>
										<StatusBadge status="blocked" />
									</TableCell>
									<TableCell className="text-muted-foreground">
										{b.blockedAtUnix
											? new Date(Number(b.blockedAtUnix) * 1000).toLocaleString()
											: "—"}
									</TableCell>
									<TableCell className="text-right">
										<Button size="sm" variant="secondary" onClick={() => onUnblock(b.peer)}>
											Unblock
										</Button>
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				)}
			</CardContent>
		</Card>
	);
}
