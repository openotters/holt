import { execSync } from "node:child_process";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Short commit hash baked into the footer at build time; "dev" when
// git is not available (e.g. building from a source tarball).
let commit = "dev";
try {
	commit = execSync("git rev-parse --short HEAD").toString().trim();
} catch {
	// keep "dev"
}

// The console is served by the hub from a fixed origin, so build to a
// plain relative-asset SPA. In dev, proxy the Admin gRPC service and
// the enroll endpoint to a locally-running hub (`holt hub --ui`).
const HUB_ADMIN = process.env.HOLT_ADMIN ?? "http://127.0.0.1:7001";

export default defineConfig({
  define: { __APP_COMMIT__: JSON.stringify(commit) },
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "src") },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/openotters.holt.v1.Admin": { target: HUB_ADMIN, changeOrigin: true },
      "/api": { target: HUB_ADMIN, changeOrigin: true },
    },
  },
});
