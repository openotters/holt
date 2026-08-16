import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider } from "next-themes";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Toaster } from "sonner";

import { App } from "@/App";
import { transport } from "@/lib/transport";
import "@/index.css";

const queryClient = new QueryClient({
	defaultOptions: {
		// Slow fallback only: tunnel changes arrive through the
		// WatchTunnels stream (see lib/use-tunnel-stream.ts). Focus
		// refetch is off for the same reason: switching back from the
		// terminal was refetching every query on top of the stream
		// driven refresh.
		queries: { refetchInterval: 30000, refetchOnWindowFocus: false, retry: 1 },
	},
});

createRoot(document.getElementById("root") as HTMLElement).render(
	<StrictMode>
		<ThemeProvider attribute="class" defaultTheme="dark" enableSystem disableTransitionOnChange>
			<TransportProvider transport={transport}>
				<QueryClientProvider client={queryClient}>
					<App />
					<Toaster position="top-center" richColors />
				</QueryClientProvider>
			</TransportProvider>
		</ThemeProvider>
	</StrictMode>,
);
