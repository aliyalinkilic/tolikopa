import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// Local dev proxies /api and /ws to the running backend (Caddy/API over TLS).
// Override the target with VITE_API_TARGET (e.g. https://10.0.0.10).
const target = process.env.VITE_API_TARGET || 'https://localhost'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target, secure: false, changeOrigin: true },
      '/ws': { target, secure: false, changeOrigin: true, ws: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    chunkSizeWarningLimit: 1100,
  },
})
