import { defineConfig } from "vitest/config";
import viteConfig from "./vite.config.js";

export default defineConfig(async (environment) => {
  const base = await viteConfig(environment);
  return {
    ...base,
    test: {
      environment: "node",
      include: ["./src/ui/**/*.test.js", "../../shared/frontend/**/*.test.js"],
      exclude: ["./src/ui/*.test.js"],
    },
  };
});
