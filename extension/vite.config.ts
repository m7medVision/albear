import { defineConfig } from 'vite'
import { crx } from '@crxjs/vite-plugin'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'
import manifest from './manifest.config'

export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  plugins: [react(), tailwindcss(), crx({ manifest })],
  build: {
    target: 'es2022',
    // Everything is bundled; the CSP forbids remote code (PRD 13.1).
    modulePreload: false,
  },
})
