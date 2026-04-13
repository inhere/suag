import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const hostTarget = env.AXON_HOST_ORIGIN || "http://127.0.0.1:8180";

  return {
    plugins: [react()],
    server: {
      proxy: {
        "/api": {
          target: hostTarget,
          changeOrigin: true,
        },
        "/events": {
          target: hostTarget,
          changeOrigin: true,
        },
        "/ws": {
          target: hostTarget,
          changeOrigin: true,
          ws: true,
        },
      },
    },
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: "./src/test/setup.ts",
    },
  };
});
