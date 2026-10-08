import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import process from "node:process";
import { fileURLToPath } from "node:url";

const host = process.env.TAURI_DEV_HOST;

export default defineConfig(() => ({
  plugins: [vue()],
  resolve: {
    alias: [
      { find: "vue", replacement: fileURLToPath(new URL("./node_modules/vue/dist/vue.esm-bundler.js", import.meta.url)) },
      { find: "@tauri-apps/api", replacement: fileURLToPath(new URL("./node_modules/@tauri-apps/api", import.meta.url)) },
      { find: "vue-router", replacement: fileURLToPath(new URL("./node_modules/vue-router/dist/vue-router.mjs", import.meta.url)) },
      { find: "marked", replacement: fileURLToPath(new URL("./node_modules/marked/lib/marked.esm.js", import.meta.url)) },
      { find: "dompurify", replacement: fileURLToPath(new URL("./node_modules/dompurify/dist/purify.es.mjs", import.meta.url)) },
      { find: "highlight.js/lib/common", replacement: fileURLToPath(new URL("./node_modules/highlight.js/es/common.js", import.meta.url)) },
    ],
  },
  clearScreen: false,
  server: {
    port: 1422,
    strictPort: true,
    host: host || "127.0.0.1",
    hmr: host
      ? {
          protocol: "ws",
          host,
          port: 1423,
        }
      : undefined,
    watch: {
      ignored: ["**/src-tauri/**"],
    },
  },
}));
