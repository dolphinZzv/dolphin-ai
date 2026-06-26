import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/graphql": {
        target: "http://localhost:8080",
        ws: true,
      },
      "/mcp": "http://localhost:8080",
      "/health": "http://localhost:8080",
    },
  },
});
