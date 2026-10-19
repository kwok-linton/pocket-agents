import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/api": { target: "http://127.0.0.1:8787", ws: true } } },
  build: {
    outDir: "dist",
    sourcemap: false,
    // React rarely changes; a separate chunk stays cached across UI deploys.
    rollupOptions: { output: { manualChunks: (id) => (/node_modules\/(react|react-dom|scheduler)\//.test(id) ? "react" : undefined) } },
  },
});
