import { X } from "lucide-react";
import { useEffect } from "react";

import { TrafficPanel } from "@/components/traffic-panel";
import { Button } from "@/components/ui/button";
import type { HubConfig } from "@/lib/reach";

// TrafficModal is the TrafficPanel in a dialog, opened from a peer's
// row in the tunnels table.
export function TrafficModal({
	peer,
	config,
	onClose,
}: {
	peer: string;
	config: HubConfig;
	onClose: () => void;
}) {
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
				className="flex h-[85vh] w-full max-w-4xl flex-col rounded-lg border bg-background shadow-lg"
				onClick={(e) => e.stopPropagation()}
				onKeyDown={() => {}}
				role="dialog"
				aria-modal="true"
			>
				<div className="flex items-start justify-between gap-3 border-b px-5 py-4">
					<div>
						<h2 className="flex items-center gap-2 font-semibold">
							Traffic <span className="font-mono text-sm">{peer}</span>
						</h2>
						<p className="mt-1 text-muted-foreground text-sm">
							Requests the hub carried to this peer, live. Click a row for the details. Nothing is
							stored: closing this loses them.
						</p>
					</div>
					<Button size="icon" variant="ghost" className="-mr-2 h-7 w-7 shrink-0" onClick={onClose}>
						<X className="h-4 w-4" />
					</Button>
				</div>

				<TrafficPanel config={config} peer={peer} />

				<div className="border-t px-5 py-3 text-muted-foreground text-xs">
					Durations are measured at the hub, so they include the tunnel hop. The peer logs the same
					requests without it.
				</div>
			</div>
		</div>
	);
}
