import { defineConfig } from 'vite'
import { crx } from '@crxjs/vite-plugin'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'
import { createManifest } from './build/manifest'
import { resolveTarget, runtimeBuild } from './build/target'

// ALBEAR_ENV / ALBEAR_VERSION pick the build target; with neither, dev.
const target = resolveTarget(process.env)

export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  define: {
    // Read through src/build-info.ts only.
    __ALBEAR_BUILD__: JSON.stringify(runtimeBuild(target)),
  },
  plugins: [react(), tailwindcss(), crx({ manifest: createManifest(target) })],
  build: {
    target: 'es2022',
    // Everything is bundled; the CSP forbids remote code (PRD 13.1).
    modulePreload: false,
  },
})
