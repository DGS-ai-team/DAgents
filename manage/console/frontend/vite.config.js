import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

const manageTarget = process.env.DAGENTS_MANAGE_URL || "http://127.0.0.1:8020";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@shared-icons": fileURLToPath(new URL("../../../shared/icons", import.meta.url)),
      "@dagents-brand": fileURLToPath(new URL("../../../shared/branding", import.meta.url)),
      "@shared-frontend": fileURLToPath(new URL("../../../shared/frontend", import.meta.url)),
    },
  },
  base: "/console/",
  build: {
    outDir: fileURLToPath(new URL("../static", import.meta.url)),
    emptyOutDir: true,
    rollupOptions: {
      input: {
        console: fileURLToPath(new URL("./index.html", import.meta.url)),
        product: fileURLToPath(new URL("./product.html", import.meta.url)),
      },
    },
  },
  server: {
    fs: { allow: [fileURLToPath(new URL("../../../", import.meta.url))] },
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
