import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  plugins: [vue()],
  base: "/console/",
  build: {
    outDir: fileURLToPath(new URL("../static", import.meta.url)),
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/v1": { target: "http://127.0.0.1:8020", changeOrigin: true },
      "/health": { target: "http://127.0.0.1:8020", changeOrigin: true },
    },
  },
});
