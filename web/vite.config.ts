import { defineConfig } from "vite";
import { resolve } from "node:path";

// Backend the dev server proxies API and SSE calls to. Run `go run ./cmd/pidash -mock`.
const backend = process.env.PIDASH_BACKEND ?? "http://127.0.0.1:8080";

export default defineConfig({
  build: {
    target: "chrome120",
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      input: {
        index: resolve(__dirname, "index.html"),
        remote: resolve(__dirname, "remote.html"),
        photos: resolve(__dirname, "photos.html"),
        settings: resolve(__dirname, "settings.html"),
      },
    },
  },
  server: {
    host: true,
    port: 5173,
    proxy: {
      "/events": { target: backend, changeOrigin: true },
      "/api": { target: backend, changeOrigin: true },
      "/photos": { target: backend, changeOrigin: true },
    },
  },
});
