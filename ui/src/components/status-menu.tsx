import { ChevronDown, Radio } from "lucide-react";
import { useEffect, useRef, useState } from "react";

// StatusMenu is the connection item in the top navbar: a real menu
// (click to open, stays open, text selectable), not a hover tooltip.
export function StatusMenu({ error, updatedAt }: { error: unknown; updatedAt: number }) {
	const [open, setOpen] = useState(false);
	const root = useRef<HTMLDivElement>(null);

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

	const row = "flex justify-between gap-6";
	const key = "text-muted-foreground";

	return (
		<div className="relative ml-auto" ref={root}>
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
				<div className="absolute top-full right-0 z-50 mt-2 w-80 select-text rounded-md border bg-popover p-4 text-popover-foreground text-sm shadow-md">
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
						<div className={row}>
							<span className={key}>service</span>
							<span className="font-mono">openotters.holt.v1.Admin</span>
						</div>
						<div className={row}>
							<span className={key}>protocol</span>
							<span>Connect (JSON) over HTTP</span>
						</div>
						<div className={row}>
							<span className={key}>last update</span>
							<span>{updatedAt ? new Date(updatedAt).toLocaleTimeString() : "never"}</span>
						</div>
						{error instanceof Error && (
							<div className="mt-1 border-t border-dashed pt-2 text-red-500">{error.message}</div>
						)}
					</div>
				</div>
			)}
		</div>
	);
}
