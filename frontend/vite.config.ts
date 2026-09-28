import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import istanbul from 'vite-plugin-istanbul'

const apiTarget = process.env.PROVENLY_API_URL ?? 'http://localhost:8080'
const coverage = process.env.VITE_COVERAGE === 'true'
const proxy = {
  '/api': { target: apiTarget, changeOrigin: true },
  '/healthz': { target: apiTarget, changeOrigin: true },
}

// VITE_COVERAGE=true builds an istanbul-instrumented bundle so E2E runs
// (Playwright) can collect frontend coverage evidence from window.__coverage__.
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    ...(coverage
      ? [
          istanbul({
            include: 'src/*',
            exclude: ['node_modules', 'src/**/*.test.*', 'src/test/**', 'src/contract/**'],
            extension: ['.ts', '.tsx'],
            forceBuildInstrument: true,
          }),
        ]
      : []),
  ],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },
  build: { sourcemap: coverage },
  server: { port: 5173, proxy },
  preview: { port: 4173, proxy },
})
