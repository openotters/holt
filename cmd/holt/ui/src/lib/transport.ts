import { createConnectTransport } from "@connectrpc/connect-web";

// The console is served by the hub from the same origin as the Admin
// service, so requests are relative (Connect-JSON, DevTools-friendly).
// In dev, Vite proxies /openotters.holt.v1.Admin and /api to a local
// hub (see vite.config.ts).
export const transport = createConnectTransport({
	baseUrl: "/",
	useBinaryFormat: false,
});
