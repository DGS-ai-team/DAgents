import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

const manageTarget = process.env.DAGENTS_MANAGE_URL || "http://127.0.0.1:8020";

export default defineConfig({
  plugins: [vue()],
  base: "/console/",
  build: {
    outDir: fileURLToPath(new URL("../static", import.meta.url)),
    emptyOutDir: true,
  },
  server: {
    host: true,
    port: Number(process.env.CONSOLE_DEV_PORT || 5174),
    strictPort: true,
    open: "/console/",
    proxy: {
      "/v1": {
        target: manageTarget,
        changeOrigin: true,
        ws: true,
      },
      "/health": { target: manageTarget, changeOrigin: true },
      "/metrics": { target: manageTarget, changeOrigin: true },
    },
  },
});
