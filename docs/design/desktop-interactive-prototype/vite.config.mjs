import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const prototypeRoot = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(prototypeRoot, "../../..");

export default defineConfig({
  build: {
    outDir: "dist/client",
  },
  optimizeDeps: {
    include: ["react", "react-dom/client"],
  },
  server: {
    fs: { allow: [repoRoot] },
    host: "0.0.0.0",
    allowedHosts: ["terminal.local"],
    warmup: {
      clientFiles: ["./src/main.jsx"],
    },
  },
  resolve: { alias: { "@shared-icons": resolve(repoRoot, "shared/icons") } },
  plugins: [react()],
});
