import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";

export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

// JsonView renders a value as a collapsible tree, the way a log viewer
// shows a structured entry.
export function JsonView({ value, className = "" }: { value: JsonValue; className?: string }) {
	return (
		<div className={`font-mono text-xs leading-relaxed ${className}`}>
			<Node value={value} depth={0} />
		</div>
	);
}

function Node({ name, value, depth, last = true }: { name?: string; value: JsonValue; depth: number; last?: boolean }) {
	const [open, setOpen] = useState(depth < 2);

	if (value === null || typeof value !== "object") {
		return (
			<Line depth={depth}>
				<Key name={name} />
				<Leaf value={value} />
				{!last && <Punct>,</Punct>}
			</Line>
		);
	}

	const entries: [string, JsonValue][] = Array.isArray(value)
		? value.map((v, i) => [String(i), v])
		: Object.entries(value);
	const [openBrace, closeBrace] = Array.isArray(value) ? ["[", "]"] : ["{", "}"];
	const Chevron = open ? ChevronDown : ChevronRight;

	return (
		<>
			<Line depth={depth}>
				<button
					className="-ml-4 mr-1 inline-flex items-center text-muted-foreground hover:text-foreground"
					onClick={() => setOpen(!open)}
					type="button"
				>
					<Chevron className="h-3 w-3" />
				</button>
				<Key name={name} />
				<Punct>{openBrace}</Punct>
				{!open && (
					<>
						<span className="text-muted-foreground">{entries.length}</span>
						<Punct>
							{closeBrace}
							{last ? "" : ","}
						</Punct>
					</>
				)}
			</Line>
			{open && (
				<>
					{entries.map(([key, child], i) => (
						<Node key={key} depth={depth + 1} last={i === entries.length - 1} name={key} value={child} />
					))}
					<Line depth={depth}>
						<Punct>
							{closeBrace}
							{last ? "" : ","}
						</Punct>
					</Line>
				</>
			)}
		</>
	);
}

// Line leaves room on the left for the fold arrow so keys stay aligned
// whether or not they have one; punctuation carries its own spacing.
function Line({ children, depth }: { children: React.ReactNode; depth: number }) {
	return (
		<div className="flex items-baseline whitespace-pre" style={{ paddingLeft: `${depth * 1.25 + 1}rem` }}>
			{children}
		</div>
	);
}

function Key({ name }: { name?: string }) {
	if (name === undefined) return null;

	return (
		<span>
			<span className="text-foreground/80">{name}</span>
			<Punct>: </Punct>
		</span>
	);
}

// Leaf colours by type, so a value's kind reads before its content.
function Leaf({ value }: { value: string | number | boolean | null }) {
	if (typeof value === "string") {
		return <span className="break-all text-sky-400">"{value}"</span>;
	}

	if (typeof value === "number") {
		return <span className="text-emerald-500">{value}</span>;
	}

	if (typeof value === "boolean") {
		return <span className="text-violet-400">{String(value)}</span>;
	}

	return <span className="text-muted-foreground">null</span>;
}

function Punct({ children }: { children: React.ReactNode }) {
	return <span className="text-muted-foreground">{children}</span>;
}
