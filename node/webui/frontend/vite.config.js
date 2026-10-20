import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@shared-icons": fileURLToPath(new URL("../../../shared/icons", import.meta.url)),
      "@dagents-brand": fileURLToPath(new URL("../../../shared/branding", import.meta.url)),
      // Desktop reuses WorkspaceFiles.vue in a cross-workspace component test.
      // Resolve this dependency from the shared UI package root because Vite
      // otherwise searches upward from desktop/ui and misses this package.
      "dompurify": fileURLToPath(new URL("./node_modules/dompurify/dist/purify.es.mjs", import.meta.url)),
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.js"],
  },
  server: {
    fs: { allow: [fileURLToPath(new URL("../../../", import.meta.url))] },
  },
});
