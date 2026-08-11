import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

const nodeTarget = process.env.DAGENTS_NODE_URL || "http://127.0.0.1:18765";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@dagents-brand": fileURLToPath(new URL("../../../shared/branding", import.meta.url)),
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.js"],
  },
  base: "/ui/",
  build: {
    outDir: fileURLToPath(new URL("../../internal/webui/static", import.meta.url)),
    emptyOutDir: true,
  },
  server: {
    host: true,
    port: Number(process.env.WEBUI_DEV_PORT || 5173),
    strictPort: true,
    open: "/ui/",
    proxy: {
      "/v1": {
        target: nodeTarget,
        changeOrigin: true,
        configure(proxy) {
          proxy.on("proxyRes", (proxyRes, req) => {
            if (req.url?.includes("/streams")) {
              proxyRes.headers["cache-control"] = "no-cache";
              proxyRes.headers["connection"] = "keep-alive";
            }
          });
        },
      },
      "/health": { target: nodeTarget, changeOrigin: true },
    },
  },
});
