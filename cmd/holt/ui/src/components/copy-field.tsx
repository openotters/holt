import { Check, Copy } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

// CopyField shows a labelled, copyable mono value in the openotters
// command-chip style. With multiline, the value wraps across lines and
// scrolls if tall (for the long join token); the copy button always
// copies the clean single-line value.
export function CopyField({
	label,
	value,
	multiline = false,
}: {
	label: string;
	value: string;
	multiline?: boolean;
}) {
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
				{/* break-words, not break-all: a curl command wraps at its
				    spaces, while a token (one unbroken string) still wraps. */}
				<code
					className={multiline ? "max-h-32 flex-1 overflow-auto whitespace-pre-wrap break-words py-1" : "truncate"}
				>
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
