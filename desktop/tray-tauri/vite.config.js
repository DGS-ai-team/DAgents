import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import process from "node:process";
import { fileURLToPath } from "node:url";

const host = process.env.TAURI_DEV_HOST;

export default defineConfig(() => ({
  plugins: [vue()],
  resolve: {
    alias: [
      { find: "@shared-icons", replacement: fileURLToPath(new URL("../../shared/icons", import.meta.url)) },
      { find: "@dagents-brand", replacement: fileURLToPath(new URL("../../shared/branding", import.meta.url)) },
      { find: /^morphicons\/vue$/, replacement: fileURLToPath(new URL("./node_modules/morphicons/dist/vue.js", import.meta.url)) },
      { find: /^morphicons\/adapters$/, replacement: fileURLToPath(new URL("./node_modules/morphicons/dist/adapters.js", import.meta.url)) },
      { find: "vue", replacement: fileURLToPath(new URL("./node_modules/vue/dist/vue.esm-bundler.js", import.meta.url)) },
      { find: "@tauri-apps/api", replacement: fileURLToPath(new URL("./node_modules/@tauri-apps/api", import.meta.url)) },
      { find: "vue-router", replacement: fileURLToPath(new URL("./node_modules/vue-router/dist/vue-router.mjs", import.meta.url)) },
      { find: "marked", replacement: fileURLToPath(new URL("./node_modules/marked/lib/marked.esm.js", import.meta.url)) },
      { find: "dompurify", replacement: fileURLToPath(new URL("./node_modules/dompurify/dist/purify.es.mjs", import.meta.url)) },
      { find: "highlight.js/lib/common", replacement: fileURLToPath(new URL("./node_modules/highlight.js/es/common.js", import.meta.url)) },
      { find: "@xterm/xterm/css/xterm.css", replacement: fileURLToPath(new URL("./node_modules/@xterm/xterm/css/xterm.css", import.meta.url)) },
      { find: "@xterm/xterm", replacement: fileURLToPath(new URL("./node_modules/@xterm/xterm/lib/xterm.mjs", import.meta.url)) },
      { find: "@xterm/addon-fit", replacement: fileURLToPath(new URL("./node_modules/@xterm/addon-fit/lib/addon-fit.mjs", import.meta.url)) },
    ],
  },
  clearScreen: false,
  server: {
    fs: { allow: [fileURLToPath(new URL("../../", import.meta.url))] },
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
