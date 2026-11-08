/// <reference types="vitest" />
import { writeFileSync } from 'node:fs'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'

// The Go binary embeds web/dist (web/embed.go); keep a tracked placeholder so the module
// compiles from a fresh clone before the web app is built.
const keepDist: Plugin = {
  name: 'keep-dist-placeholder',
  closeBundle() {
    writeFileSync(new URL('./dist/.gitkeep', import.meta.url), '')
  },
}

export default defineConfig({
  plugins: [react(), keepDist],
  // Bootstrap 5.3's Sass still uses @import and global functions: its own deprecation notes are muted.
  css: { preprocessorOptions: { scss: { quietDeps: true, silenceDeprecations: ['import', 'global-builtin', 'color-functions', 'mixed-decls', 'if-function'] } } },
  build: { outDir: 'dist', emptyOutDir: true, sourcemap: false },
  server: { proxy: { '/api': 'http://localhost:8080' } },
  test: { environment: 'jsdom', setupFiles: ['./src/test/setup.ts'], globals: true },
})
