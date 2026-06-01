import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

// The Go binary embeds internal/web/dist/ via go:embed at build time.
// Writing the Vite output there directly means `make build` just works
// without an extra copy step.
//
// `base: '/admin/'` ensures every emitted asset URL is prefixed with
// /admin/, so the SPA can be served from that subpath without rewriting
// asset references at runtime.
export default defineConfig({
  // Served at root so the one bundle works under both /admin/* (console)
  // and /portal/* (user portal); assets live at /assets/*.
  base: '/',
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    // During `vite dev`, proxy API calls to the Go gateway so the SPA
    // can use relative URLs like fetch('/admin/api/login').
    proxy: {
      '/admin/api': 'http://localhost:8080',
    },
  },
})
