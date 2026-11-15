import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "/",
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
    target: "es2020",
    chunkSizeWarningLimit: 600,
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
