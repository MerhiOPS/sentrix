/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The server embeds web/dist with go:embed, so the build output location is
// part of the server's contract. The dev server proxies the API to a local
// sentrix-server.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    proxy: { "/api": { target: "http://127.0.0.1:8080", ws: true } },
  },
  // Component tests render into jsdom; browser tests (e2e/) run in Playwright.
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
