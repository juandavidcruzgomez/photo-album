import path from "node:path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  server: {
    proxy: { "/api": process.env.PHALBUM_API ?? "http://localhost:8090" },
  },
  build: {
    // dist/ is embedded into the Go binary; keep the placeholder go:embed needs.
    emptyOutDir: true,
  },
})
