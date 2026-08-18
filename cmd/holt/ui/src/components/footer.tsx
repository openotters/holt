declare const __APP_COMMIT__: string;

function ExtLink({ href, children }: { href: string; children: React.ReactNode }) {
	return (
		<a
			className="font-medium text-foreground transition-colors hover:text-muted-foreground"
			href={href}
			rel="noreferrer"
			target="_blank"
		>
			{children}
		</a>
	);
}

export function Footer() {
	return (
		<footer className="mt-auto border-t border-dashed">
			<div className="mx-auto flex max-w-5xl flex-col items-center gap-3 px-6 py-12 text-center">
				<a
					className="flex items-center gap-2 font-semibold"
					href="https://github.com/openotters/holt"
					rel="noreferrer"
					target="_blank"
				>
					<span aria-hidden="true">🌀</span> holt
				</a>
				<p className="max-w-md text-muted-foreground text-sm">
					Reverse HTTP tunnels for services that can only dial out. A WebSocket carrier that passes
					anywhere, JWT out of the box, live traffic capture, presence for free.
				</p>
			</div>

			<div className="border-t border-dashed">
				<div className="mx-auto flex w-full max-w-5xl flex-col items-center justify-between gap-2 px-6 py-4 text-muted-foreground text-sm sm:flex-row">
					<span>
						© {new Date().getFullYear()}{" "}
						<ExtLink href="https://github.com/openotters/holt">
							<span aria-hidden="true">🌀</span> holt
						</ExtLink>{" "}
						<span className="font-mono text-xs">#{__APP_COMMIT__}</span> • Reverse tunnels from the den,
						forever
					</span>
					<span>
						Built by <ExtLink href="https://github.com/merlindorin">Merlindorin</ExtLink> · See also{" "}
						<ExtLink href="https://openotters.io">OpenOtters</ExtLink> ·{" "}
						<ExtLink href="https://sshark.app">sshark</ExtLink>
					</span>
				</div>
			</div>
		</footer>
	);
}
