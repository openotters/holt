import { cn } from "@/lib/utils";

// Mirrors openotters' StatusBadge visual language: a /10-tinted pill
// with a colored dot, transitional states pulsing.
type Visual = { label: string; className: string; dot: string };

const config: Record<string, Visual> = {
	attached: { label: "Attached", className: "bg-emerald-500/10 text-emerald-500", dot: "bg-emerald-500" },
	connecting: { label: "Connecting", className: "bg-amber-500/10 text-amber-500", dot: "bg-amber-500 animate-pulse" },
	blocked: { label: "Blocked", className: "bg-red-500/10 text-red-500", dot: "bg-red-500" },
	offline: { label: "Offline", className: "bg-muted text-muted-foreground", dot: "bg-muted-foreground" },
};

const fallback: Visual = { label: "Unknown", className: "bg-muted text-muted-foreground", dot: "bg-muted-foreground" };

export function StatusBadge({ status, className }: { status: string; className?: string }) {
	const v = config[status] ?? { ...fallback, label: status || fallback.label };

	return (
		<span
			className={cn(
				"inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 font-medium text-xs",
				v.className,
				className,
			)}
		>
			<span className={cn("h-1.5 w-1.5 rounded-full", v.dot)} />
			{v.label}
		</span>
	);
}
