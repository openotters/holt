import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import {
	Activity,
	Ban,
	BookOpen,
	Beer,
	Container,
	Download,
	ExternalLink,
	Github,
	Plus,
	Radio,
	RefreshCw,
	Ship,
	ShieldAlert,
	ShieldOff,
	Star,
	Terminal,
	X,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { CapturePage } from "@/components/capture-page";
import { CopyField } from "@/components/copy-field";
import { Footer } from "@/components/footer";
import { TrafficModal } from "@/components/traffic-modal";
import { peerURL } from "@/lib/reach";
import { StatusBadge } from "@/components/status-badge";
import { StatusMenu } from "@/components/status-menu";
import { useLiveTunnels } from "@/lib/use-tunnel-stream";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { blockPeer, info, listBlocked, listTunnels, stopTunnel, unblockPeer } from "@/gen/v1/admin-Admin_connectquery";
import type { TunnelActivity } from "@/lib/use-tunnel-stream";

// Proper connect-query keys: the v2 key shape is ["connect-query",
// {...}], so hand-written ["Service", "Method"] arrays match nothing
// and invalidations silently no-op. cardinality undefined = filter
// matching both finite and infinite queries.
const BLOCKED_KEY = createConnectQueryKey({ cardinality: undefined, schema: listBlocked });

const REPO_URL = "https://github.com/openotters/holt";
const DOCS_URL = `${REPO_URL}/blob/main/docs/README.md`;

// useHubConfig fetches the proxy port + routing header once, so the
// "call this peer" command points at the right address (the console is
// served from the admin port, not the proxy one).
function useHubConfig() {
	const [cfg, setCfg] = useState<{
		routeHeader: string;
		proxyPort: string;
		externalURL: string;
		tunnelURL: string;
		metricsPort: string;
		proxyRouting: string;
		proxyDomain: string;
	}>({
		routeHeader: "x-tunnel-peer",
		proxyPort: "7202",
		externalURL: "",
		tunnelURL: "",
		metricsPort: "",
		proxyRouting: "header",
		proxyDomain: "",
	});
	useEffect(() => {
		fetch("/api/config")
			.then((r) => (r.ok ? r.json() : null))
			.then((c) => c && setCfg(c))
			.catch(() => {});
	}, []);
	return cfg;
}

// The console's pages: operate the live peers, inspect requests,
// touch-once settings.
type Page = "tunnels" | "capture" | "settings";

const PAGES: { key: Page; label: string }[] = [
	{ key: "tunnels", label: "Tunnels" },
	{ key: "capture", label: "Capture" },
	{ key: "settings", label: "Settings" },
];

// usePage is hash routing, small enough to own: #/capture and
// #/settings deep-link; anything else is the tunnels page.
function usePage(): [Page, (p: Page) => void] {
	const read = (): Page => {
		const h = window.location.hash.replace(/^#\/?/, "");
		return h === "capture" || h === "settings" ? h : "tunnels";
	};
	const [page, setPage] = useState<Page>(read);

	useEffect(() => {
		const onHash = () => setPage(read());
		window.addEventListener("hashchange", onHash);
		return () => window.removeEventListener("hashchange", onHash);
	}, []);

	return [
		page,
		(p) => {
			window.location.hash = p === "tunnels" ? "/" : `/${p}`;
		},
	];
}

export function App() {
	const config = useHubConfig();
	const [page, go] = usePage();

	// The tunnels page runs the same query; the shared cache makes
	// this one free.
	const { error } = useQuery(listTunnels, {});

	return (
		<div className="flex min-h-screen flex-col font-sans antialiased">
			{/* Sticky so the nav and the status menu stay reachable while
			    scrolling a long peer list. Translucent + blur keeps the
			    content visible as it passes underneath; z-40 sits under the
			    status popover's z-50. */}
			<header className="sticky top-0 z-40 flex h-16 shrink-0 items-center gap-3 border-b border-dashed bg-background/80 px-6 backdrop-blur supports-[backdrop-filter]:bg-background/60">
				<a className="flex items-center gap-3" href="#/" title="back to the tunnels">
					<span className="text-xl leading-none" aria-hidden="true">
						🌀
					</span>
					<span className="font-semibold tracking-tight">holt console</span>
				</a>
				<StatusMenu
					error={error}
					proxyURL={config.externalURL || `http://${window.location.hostname}:${config.proxyPort}`}
					tunnelURL={config.tunnelURL}
				/>
				<nav className="ml-2 flex items-center gap-1">
					{PAGES.map((p) => (
						<button
							key={p.key}
							className={`h-8 rounded-md px-3 text-sm transition-colors ${
								page === p.key
									? "bg-accent font-medium text-accent-foreground"
									: "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
							}`}
							onClick={() => go(p.key)}
							type="button"
						>
							{p.label}
						</button>
					))}
				</nav>
				<div className="ml-auto flex items-center gap-1.5">
					<a
						className="inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-muted-foreground text-xs transition-colors hover:bg-accent hover:text-accent-foreground"
						href={DOCS_URL}
						rel="noreferrer"
						target="_blank"
						title="holt documentation"
					>
						<BookOpen className="h-3.5 w-3.5" /> Docs
					</a>
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
				</div>
			</header>

			<main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 p-6">
				{page === "tunnels" && <TunnelsPage config={config} />}
				{page === "capture" && <CapturePage config={config} />}
				{page === "settings" && <SettingsPage config={config} />}
			</main>

			<Footer />
		</div>
	);
}

// HubViewConfig is everything /api/config reports.
type HubViewConfig = ReturnType<typeof useHubConfig>;

// TunnelsPage is the operating view: live peers, activity, bans.
function TunnelsPage({ config }: { config: HubViewConfig }) {
	const queryClient = useQueryClient();
	const [callPeer, setCallPeer] = useState<string | null>(null);
	const [trafficPeer, setTrafficPeer] = useState<string | null>(null);
	const [confirmBlock, setConfirmBlock] = useState<string | null>(null);
	const [addPeer, setAddPeer] = useState(false);

	const { data, error, isLoading } = useQuery(listTunnels, {});
	const { data: hubInfo } = useQuery(info, {});
	const hubMinor = hubInfo ? minorVersion(hubInfo.version) : null;

	// The uptime column is a duration, so it drifts even when nothing
	// happens; a slow tick keeps it honest without event traffic.
	const [, setUptimeTick] = useState(0);
	useEffect(() => {
		const t = setInterval(() => setUptimeTick((n) => n + 1), 30_000);
		return () => clearInterval(t);
	}, []);

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
		<>
			<div>
				<h1 className="font-semibold text-2xl tracking-tight">Tunnels</h1>
					<p className="text-muted-foreground text-sm">
						Live reverse tunnels attached to this hub. Kill disconnects a peer (it may reconnect); block
						also bans its peer id so no token for it works until unblocked.
					</p>
				</div>

				<Card>
					<CardHeader>
						{/* The panel owns its action: adding a peer belongs to
						    the list it lands in, not to a form above it. */}
						<div className="flex items-center justify-between gap-3">
							<CardTitle>
								Attached peers
								<span className="ml-2 font-normal text-muted-foreground text-sm">
									{tunnels.length > 0 ? `${tunnels.length}` : ""}
								</span>
							</CardTitle>
							<Button size="sm" onClick={() => setAddPeer(true)}>
								<Plus className="h-3.5 w-3.5" /> Add peer
							</Button>
						</div>
					</CardHeader>
					<CardContent>
						{error ? (
							<div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-sm">
								Could not reach the hub Admin API: {error.message}
							</div>
						) : isLoading ? (
							<p className="py-8 text-center text-muted-foreground text-sm">loading…</p>
						) : tunnels.length === 0 ? (
							<div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed py-12 text-muted-foreground text-sm">
								<span>no peers attached</span>
								<span>on the machine you want to expose, run:</span>
								<code className="rounded-md border bg-muted/50 px-3 py-1 font-mono text-xs">
									holt expose localhost:3000
								</code>
								<span className="text-xs">it enrolls itself, or take a token with Add peer</span>
							</div>
						) : (
							<Table>
								<TableHeader>
									<TableRow>
										<TableHead>Peer</TableHead>
										<TableHead>Type</TableHead>
										<TableHead>Status</TableHead>
										<TableHead>Version</TableHead>
										<TableHead>Attached</TableHead>
										<TableHead className="text-right">Actions</TableHead>
									</TableRow>
								</TableHeader>
								<TableBody>
									{tunnels.map((t) => {
										const peerMinor = minorVersion(t.peerVersion || "");
										const skewed = Boolean(hubMinor && peerMinor && hubMinor !== peerMinor);
										const url = peerURL(t.peer, config.proxyDomain);
										return (
											<TableRow key={t.peer}>
												<TableCell className="font-mono font-medium">
													{url ? (
														// Subdomain routing gives the peer a real URL, so the
														// name is the way to it.
														<a
															className="inline-flex items-center gap-1.5 hover:underline"
															href={url}
															rel="noreferrer"
															target="_blank"
															title={url}
														>
															{t.peer}
															<ExternalLink className="h-3 w-3 text-muted-foreground" />
														</a>
													) : (
														t.peer
													)}
												</TableCell>
												<TableCell className="text-muted-foreground">
													{t.tunnelType || "http"}
												</TableCell>
												<TableCell>
													<StatusBadge status="attached" />
												</TableCell>
												<TableCell className={skewed ? "text-amber-500" : "text-muted-foreground"}>
													<span
														title={
															skewed
																? `peer built for ${peerMinor}, hub is ${hubMinor}; consider upgrading the peer`
																: undefined
														}
													>
														{t.peerVersion || "—"}
													</span>
												</TableCell>
												<TableCell className="text-muted-foreground">
													{t.attachedAtUnix ? (
														<span title={new Date(Number(t.attachedAtUnix) * 1000).toLocaleString()}>
															{formatUptime(t.attachedAtUnix)}
														</span>
													) : (
														"—"
													)}
												</TableCell>
												<TableCell className="text-right">
													{confirmBlock === t.peer ? (
														<div className="inline-flex items-center gap-1.5">
															<Button
																size="sm"
																variant="destructive"
																onClick={() => {
																	block.mutate({ peer: t.peer });
																	setConfirmBlock(null);
																}}
															>
																<Ban className="h-3.5 w-3.5" /> Confirm block
															</Button>
															<Button size="sm" variant="secondary" onClick={() => setConfirmBlock(null)}>
																Cancel
															</Button>
														</div>
													) : (
														<div className="inline-flex items-center gap-1.5">
															{/* Per peer, not hub-wide: a fleet's traffic in one
															    list is unreadable, and this one is filtered by
															    the hub rather than by the browser. */}
															<Button size="sm" variant="outline" onClick={() => setTrafficPeer(t.peer)}>
																<Radio className="h-3.5 w-3.5" /> Traffic
															</Button>
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
															<Button size="sm" variant="destructive" onClick={() => setConfirmBlock(t.peer)}>
																<Ban className="h-3.5 w-3.5" /> Block
															</Button>
														</div>
													)}
												</TableCell>
											</TableRow>
										);
									})}
								</TableBody>
							</Table>
						)}
					</CardContent>
				</Card>

				<ActivityCard activity={stream.activity} />

				<BlockedCard onUnblock={(peer) => unblock.mutate({ peer })} />

			{addPeer && <AddPeerModal onClose={() => setAddPeer(false)} />}

			{trafficPeer && (
				<TrafficModal config={config} peer={trafficPeer} onClose={() => setTrafficPeer(null)} />
			)}

			{callPeer && (
				<CallPeerModal
					peer={callPeer}
					routeHeader={config.routeHeader}
					proxyPort={config.proxyPort}
					externalURL={config.externalURL}
					proxyDomain={config.proxyDomain}
					proxyRouting={config.proxyRouting}
					onClose={() => setCallPeer(null)}
				/>
			)}
		</>
	);
}

// SettingsPage holds what you touch once: wiring, install, danger zone.
function SettingsPage({ config }: { config: HubViewConfig }) {
	return (
		<>
			<div>
				<h1 className="font-semibold text-2xl tracking-tight">Settings</h1>
				<p className="text-muted-foreground text-sm">
					How this hub is configured, install methods for new machines, and the actions that need a
					second thought.
				</p>
			</div>

			<HubCard config={config} />

			<InstallCard />

			<DangerZone />
		</>
	);
}

// HubCard reads the hub's wiring back from /api/config: the values
// tokens and commands are built from. Flags change them, nothing here.
function HubCard({ config }: { config: HubViewConfig }) {
	const rows: { label: string; value: string; hint?: string }[] = [
		{
			label: "Proxy routing",
			value: config.proxyRouting + (config.proxyDomain ? ` (${config.proxyDomain})` : ""),
			hint:
				config.proxyRouting === "header"
					? `requests name their peer with the ${config.routeHeader} header`
					: "peers are addressed as hostnames under the proxy domain",
		},
		{ label: "Proxy port", value: config.proxyPort },
		{
			label: "External URL",
			value: config.externalURL || "—",
			hint: config.externalURL ? undefined : "set --external-url to show public commands",
		},
		{ label: "Tunnel URL", value: config.tunnelURL || "—" },
		{
			label: "Metrics",
			value: config.metricsPort ? `port ${config.metricsPort}` : "off",
		},
	];

	return (
		<Card>
			<CardHeader>
				<CardTitle>This hub</CardTitle>
				<CardDescription>Read from the hub's configuration; change it with flags on holt hub.</CardDescription>
			</CardHeader>
			<CardContent>
				<div className="flex flex-col divide-y">
					{rows.map((r) => (
						<div className="flex flex-col gap-0.5 py-2.5 first:pt-0 last:pb-0" key={r.label}>
							<div className="flex items-center justify-between gap-3">
								<span className="text-muted-foreground text-sm">{r.label}</span>
								<span className="truncate font-mono text-sm" title={r.value}>
									{r.value}
								</span>
							</div>
							{r.hint && <span className="text-muted-foreground text-xs">{r.hint}</span>}
						</div>
					))}
				</div>
			</CardContent>
		</Card>
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
	proxyDomain,
	proxyRouting,
	onClose,
}: {
	peer: string;
	routeHeader: string;
	proxyPort: string;
	externalURL: string;
	proxyDomain: string;
	proxyRouting: string;
	onClose: () => void;
}) {
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	}, [onClose]);

	const host = window.location.hostname || "127.0.0.1";

	// With subdomain routing the peer has its own hostname, which is
	// the form anything that only takes a URL (a browser, a webhook)
	// can use, so it leads.
	const headerRouted = proxyRouting !== "subdomain";
	const subdomainURL = peerURL(peer, proxyDomain);

	// A header-routed command needs a base URL the reader can
	// actually reach: the public URL if the operator set one, else
	// the peer's own hostname (which resolves wherever subdomain
	// routing is on), else loopback — and loopback only where the
	// console IS the hub, since elsewhere the proxy port is not
	// exposed.
	const onLoopback = ["127.0.0.1", "localhost", "::1", "[::1]"].includes(host);
	const loopbackBase = `http://${host}:${proxyPort}`;
	const originBase = subdomainURL.replace(/\/$/, "");
	const headerBase =
		externalURL || originBase || (onLoopback || !subdomainURL ? loopbackBase : "");

	const snippets: { label: string; value: string; hint?: string }[] = [];

	if (subdomainURL) {
		snippets.push({
			label: "curl",
			value: `curl ${subdomainURL}`,
			hint: "its own hostname, no header needed",
		});
	}

	if (headerRouted && headerBase) {
		snippets.push({
			label: `curl with the ${routeHeader} header`,
			value: `curl -H '${routeHeader}: ${peer}' ${headerBase}/`,
			hint: subdomainURL
				? "the header wins over the hostname, so one base URL reaches any peer"
				: "names the peer explicitly, from any client that can set a header",
		});
	}

	// A body example, addressed the simplest way the hub allows.
	const postBase = subdomainURL || (headerBase ? `${headerBase}/` : "");
	if (postBase) {
		const headerFlag = subdomainURL ? "" : `-H '${routeHeader}: ${peer}' `;
		snippets.push({
			label: "POST a body",
			value: `curl -X POST ${headerFlag}-H 'content-type: application/json' \\\n  -d '{"hello":"peer"}' ${postBase}`,
			hint: "any method, headers, and body reach the peer as sent",
		});
	}

	return (
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
			onClick={onClose}
			onKeyDown={() => {}}
			role="presentation"
		>
			<div
				className="flex max-h-[85vh] w-full max-w-xl flex-col rounded-lg border bg-background shadow-lg"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={() => {}}
				role="dialog"
				aria-modal="true"
			>
				<div className="flex items-start justify-between border-b px-5 py-4">
					<div>
						<h2 className="flex items-center gap-2 font-semibold">
							<Terminal className="h-4 w-4" /> Call <span className="font-mono text-sm">{peer}</span>
						</h2>
						<p className="mt-1 text-muted-foreground text-sm">
							Everything below reaches the handler this peer attached with, through the hub.
						</p>
					</div>
					<Button size="icon" variant="ghost" className="-mr-2 h-7 w-7 shrink-0" onClick={onClose}>
						<X className="h-4 w-4" />
					</Button>
				</div>

				<div className="flex flex-col gap-4 overflow-auto px-5 py-4">
					{subdomainURL && (
						<div className="flex flex-col gap-2">
							<div className="flex items-center justify-between gap-3">
								<code className="truncate font-mono text-sm">{subdomainURL}</code>
								<Button asChild size="sm" variant="outline" className="shrink-0">
									<a href={subdomainURL} rel="noreferrer" target="_blank">
										<ExternalLink className="h-3.5 w-3.5" /> Open
									</a>
								</Button>
							</div>
							<p className="text-muted-foreground text-xs">
								This peer's own hostname. Any client that takes a URL works: a browser, a webhook
								sender, an OAuth callback.
							</p>
						</div>
					)}

					{snippets.map((s) => (
						<div className="flex flex-col gap-1" key={s.label}>
							<CopyField label={s.label} value={s.value} multiline />
							{s.hint && <span className="text-muted-foreground text-xs">{s.hint}</span>}
						</div>
					))}
				</div>

				<div className="border-t px-5 py-3 text-muted-foreground text-xs">
					{subdomainURL && headerRouted ? (
						<>
							This hub routes both ways: by hostname, and by the{" "}
							<code className="font-mono">{routeHeader}</code> header when a client sets it.
						</>
					) : subdomainURL ? (
						<>This hub routes by hostname only.</>
					) : (
						<>
							This hub routes by the <code className="font-mono">{routeHeader}</code> header. Configure a
							peer domain on the hub to give each peer its own hostname.
						</>
					)}
				</div>
			</div>
		</div>
	);
}

// Install methods shown as tiles in InstallCard; clicking one opens a
// modal with the command. The enroll token is only useful once holt is
// installed on the peer.
type InstallMethod = {
	key: string;
	label: string;
	icon: LucideIcon;
	blurb: string;
	command: string;
	note?: string;
};

const INSTALL_METHODS: InstallMethod[] = [
	{
		key: "brew",
		label: "Homebrew",
		icon: Beer,
		blurb: "macOS and Linux, via the openotters tap.",
		command: "brew install openotters/tap/holt",
	},
	{
		key: "binary",
		label: "Go / binary",
		icon: Terminal,
		blurb: "Install with Go, or grab a prebuilt binary from the releases page.",
		command: "go install github.com/openotters/holt/cmd/holt@latest",
	},
	{
		key: "docker",
		label: "Docker",
		icon: Container,
		blurb: "Multi-arch image (amd64/arm64) on ghcr.",
		command: "docker run --rm ghcr.io/openotters/holt:latest --version",
	},
	{
		key: "kube",
		label: "Kubernetes",
		icon: Ship,
		blurb: "Helm chart published as an OCI artifact.",
		command: "helm install holt oci://ghcr.io/openotters/charts/holt",
	},
];

// InstallCard is collapsed by default: installing holt is a one time
// step per peer, so it should not take space above the tunnels the
// operator came to look at.
// InstallCard is reference material rather than an action: how to get
// holt on a machine, whichever role it plays there (a peer to expose,
// or a hub to run). It sits low on the page and stays open, since a
// collapsed card of four tiles is mostly padding.
function InstallCard() {
	const [selected, setSelected] = useState<InstallMethod | null>(null);

	return (
		<Card>
			<CardHeader>
				<CardTitle className="flex items-center gap-2">
					<Download className="h-4 w-4 text-muted-foreground" /> Install holt
				</CardTitle>
				<CardDescription>
					The same binary is the hub and the peer. Pick how to get it on the machine.
				</CardDescription>
			</CardHeader>
			<CardContent>
				<div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
					{INSTALL_METHODS.map((m) => (
						<button
							key={m.key}
							className="flex flex-col items-center gap-2 rounded-lg border p-4 text-center transition-colors hover:border-border hover:bg-accent hover:text-accent-foreground"
							type="button"
							onClick={() => setSelected(m)}
						>
							<m.icon className="h-6 w-6" />
							<span className="font-medium text-sm">{m.label}</span>
						</button>
					))}
				</div>
			</CardContent>

			{selected && <InstallModal method={selected} onClose={() => setSelected(null)} />}
		</Card>
	);
}

// InstallModal shows a single install method's command with a copy
// button. Closes on Escape, the close button, or a backdrop click.
function InstallModal({ method, onClose }: { method: InstallMethod; onClose: () => void }) {
	useEffect(() => {
		const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	}, [onClose]);

	const Icon = method.icon;

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
						<Icon className="h-4 w-4" /> Install with {method.label}
					</h2>
					<Button size="icon" variant="ghost" className="h-7 w-7" onClick={onClose}>
						<X className="h-4 w-4" />
					</Button>
				</div>
				<p className="mb-3 text-muted-foreground text-sm">{method.blurb}</p>
				<CopyField label="Run this on the peer" value={method.command} multiline />
				<p className="mt-3 text-muted-foreground text-xs">
					Prebuilt binaries for every OS and arch are on the{" "}
					<a
						className="font-medium text-foreground hover:text-accent-foreground"
						href={`${REPO_URL}/releases`}
						rel="noreferrer"
						target="_blank"
					>
						releases page
					</a>
					.
				</p>
			</div>
		</div>
	);
}

// EnrollCard mints a join token via the hub's /api/enroll endpoint and
// shows both the raw token (for any client) and the ready-to-run
// expose command (to tunnel a local endpoint).
// AddPeerModal is the whole "add a peer" flow: name it, take the
// token, and see how to install holt on the machine. It lives in a
// modal because enrolling is an action taken now and then, not a panel
// the operator needs on screen while watching tunnels.
function AddPeerModal({ onClose }: { onClose: () => void }) {
	const [peer, setPeer] = useState("");
	const [result, setResult] = useState<{ token: string; command: string } | null>(null);

	const name = peer.trim();
	const nameError = name ? peerNameError(name) : null;

	useEffect(() => {
		const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	}, [onClose]);

	async function enroll() {
		if (!name || nameError) return;

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
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
			onClick={onClose}
			onKeyDown={() => {}}
			role="presentation"
		>
			<div
				className="flex max-h-[85vh] w-full max-w-xl flex-col rounded-lg border bg-background shadow-lg"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={() => {}}
				role="dialog"
				aria-modal="true"
			>
				<div className="flex items-start justify-between border-b px-5 py-4">
					<div>
						<h2 className="font-semibold">Add a peer</h2>
						<p className="mt-1 text-muted-foreground text-sm">
							Name it, then run the command it gives you on the machine you want to reach.
						</p>
					</div>
					<Button size="icon" variant="ghost" className="-mr-2 h-7 w-7 shrink-0" onClick={onClose}>
						<X className="h-4 w-4" />
					</Button>
				</div>

				<div className="flex flex-col gap-4 overflow-auto px-5 py-4">
					<div className="flex flex-col gap-2">
						<div className="flex gap-2">
							{/* biome-ignore lint/a11y/noAutofocus: the modal exists to type a name */}
							<input
								autoFocus
								value={peer}
								onChange={(e) => setPeer(e.target.value)}
								onKeyDown={(e) => e.key === "Enter" && enroll()}
								placeholder="peer id, e.g. alice"
								aria-invalid={Boolean(nameError)}
								className={`h-10 flex-1 rounded-md border bg-transparent px-3 text-sm outline-none focus-visible:ring-[3px] ${
									nameError
										? "border-destructive focus-visible:border-destructive focus-visible:ring-destructive/40"
										: "focus-visible:border-ring focus-visible:ring-ring/50"
								}`}
							/>
							<Button className="h-10" onClick={enroll} disabled={!name || Boolean(nameError)}>
								Generate token
							</Button>
						</div>

						{nameError ? (
							<p className="text-destructive text-xs">{nameError}</p>
						) : (
							<p className="text-muted-foreground text-xs">
								A peer id is a DNS label (lowercase letters, digits, dashes), so it can also be
								addressed as a hostname where the proxy routes by subdomain.
							</p>
						)}
					</div>

					{result && (
						<div className="flex flex-col gap-3">
							<CopyField
								label="Token (for any client: starter-client, your own)"
								value={result.token}
								multiline
							/>
							<CopyField label="Expose a local endpoint" value={result.command} />
						</div>
					)}
				</div>
			</div>
		</div>
	);
}

// peerNameError mirrors the hub's peer-name rule (a DNS label) so the
// console can say what is wrong before the request, in the same terms
// the API would answer with. The hub validates again regardless; this
// is feedback, not enforcement.
function peerNameError(name: string): string | null {
	if (name.length > 63) return "too long: a peer id is at most 63 characters";
	if (/[A-Z]/.test(name)) {
		return `hostnames are case-insensitive, use "${name.toLowerCase()}" instead`;
	}
	if (name.includes(".")) return "no dots: that would nest another level under the proxy domain";
	if (!/^[a-z0-9-]+$/.test(name)) return "only lowercase letters, digits and dashes";
	if (name.startsWith("-") || name.endsWith("-")) return "cannot start or end with a dash";
	return null;
}

// ActivityCard lists the attaches and detaches seen by this browser's
// WatchTunnels subscription, newest first. Detaches carry the reason
// the hub sent (superseded, connection-lost, an operator kill), which
// is the answer to "why did my peer drop?" that the table alone
// cannot give: a detached peer simply vanishes from it.
function ActivityCard({ activity }: { activity: TunnelActivity[] }) {
	return (
		<Card>
			<CardHeader>
				<CardTitle className="flex items-center gap-2">
					<Activity className="h-4 w-4" /> Recent activity
					<span className="font-normal text-muted-foreground text-sm">this session</span>
				</CardTitle>
			</CardHeader>
			<CardContent>
				{activity.length === 0 ? (
					<div className="flex items-center justify-center rounded-lg border border-dashed py-8 text-muted-foreground text-sm">
						attaches and detaches will appear here as they happen
					</div>
				) : (
					<ul className="flex max-h-64 flex-col gap-1.5 overflow-auto text-sm">
						{activity.map((a) => (
							<li className="flex items-baseline gap-3" key={`${a.at}-${a.kind}-${a.peer}`}>
								<span className="shrink-0 font-mono text-muted-foreground text-xs">
									{new Date(a.at).toLocaleTimeString()}
								</span>
								<span className={a.kind === "attached" ? "text-emerald-500" : "text-red-500"}>
									{a.kind}
								</span>
								<span className="font-mono">{a.peer}</span>
								{a.reason && <span className="truncate text-muted-foreground">{a.reason}</span>}
							</li>
						))}
					</ul>
				)}
			</CardContent>
		</Card>
	);
}

// formatUptime renders the time since a unix-seconds attach as a
// compact age ("14m", "2h 14m", "3d 4h").
function formatUptime(attachedAtUnix: bigint) {
	const s = Math.max(0, Math.floor(Date.now() / 1000 - Number(attachedAtUnix)));
	const d = Math.floor(s / 86400);
	const h = Math.floor((s % 86400) / 3600);
	const m = Math.floor((s % 3600) / 60);
	if (d > 0) return `${d}d ${h}h`;
	if (h > 0) return `${h}h ${m}m`;
	if (m > 0) return `${m}m`;
	return `${s}s`;
}

// minorVersion extracts "major.minor" from a version-ish string
// ("0.15.0", "v0.15.0-dirty", "holt-expose v0.15.0"), or null when
// there is nothing parseable, so free-form peer versions never
// produce a false skew warning.
function minorVersion(s: string): string | null {
	const m = s.match(/v?(\d+\.\d+)(?:\.\d+)?/);
	return m ? m[1] : null;
}

// DangerZone holds destructive, hub-wide actions. Rotating the signing
// secret invalidates every join token (each was signed with the old
// secret), so it is gated behind an inline two-step confirmation rather
// than a one-click button.
function DangerZone() {
	const [confirming, setConfirming] = useState(false);
	const [rotating, setRotating] = useState(false);

	const rotate = async () => {
		setRotating(true);
		try {
			const res = await fetch("/api/rotate-secret", { method: "POST" });
			if (!res.ok) throw new Error(await res.text());
			const { closedTunnels = 0 } = (await res.json()) as { closedTunnels?: number };
			toast.success("Signing secret rotated", {
				description:
					`Closed ${closedTunnels} tunnel${closedTunnels === 1 ? "" : "s"}. ` +
					"Existing join tokens are now invalid, re-enroll your peers.",
			});
		} catch (e) {
			toast.error("Rotate failed", { description: e instanceof Error ? e.message : String(e) });
		} finally {
			setRotating(false);
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
						<div className="font-medium text-sm">Rotate signing secret</div>
						<p className="text-muted-foreground text-sm">
							Generates a fresh JWT signing secret and uses it immediately. Every existing join token was
							signed with the old secret and stops working, so peers must be re-enrolled.
						</p>
					</div>
					{confirming ? (
						<div className="flex shrink-0 items-center gap-2">
							<Button size="sm" variant="destructive" disabled={rotating} onClick={rotate}>
								<RefreshCw className={`h-3.5 w-3.5 ${rotating ? "animate-spin" : ""}`} />
								{rotating ? "Rotating…" : "Confirm rotate"}
							</Button>
							<Button size="sm" variant="secondary" disabled={rotating} onClick={() => setConfirming(false)}>
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
							<RefreshCw className="h-3.5 w-3.5" /> Rotate secret
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
