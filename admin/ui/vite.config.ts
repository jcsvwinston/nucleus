import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))

// Build pipeline:
//
//   * `npm run dev` (or `make ui-dev`) starts Vite on :5173 with a proxy
//     to the admin server on :8080. The proxy injects an X-Auth-User
//     header so the UI listener (which trusts 127.0.0.1) accepts the
//     request without an explicit reverse-proxy in front.
//
//   * `npm run build` (or `make ui-build`) writes the production bundle
//     directly into ../server/ui/dist so admin/server/ui/embed.go's
//     //go:embed all:dist picks it up. `make build` then produces a
//     single binary that serves the UI at "/" alongside the Connect-RPC
//     routes.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.join(here, 'src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/nucleus.admin.v1.': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: false,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('X-Auth-User', 'dev')
            proxyReq.setHeader('X-Auth-Email', 'dev@local')
          })
        },
      },
      '/healthz': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
  build: {
    // Direct output into the Go server's embed path. emptyOutDir is set
    // because we override the project default (admin/server/ui/dist/);
    // Vite needs explicit confirmation that wiping outside of project
    // root is intentional.
    outDir: path.resolve(here, '../server/ui/dist'),
    emptyOutDir: true,
    sourcemap: false,
  },
})
