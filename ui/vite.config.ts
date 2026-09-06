import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { cpSync, readdirSync } from 'fs'
import { join, resolve } from 'path'

// Entries under public/ that we deliberately skip when copying into the
// build output. Avoids Vite following the `public/results -> ../../results`
// symlink used in local dev, which would pull tens of GB of benchmark
// output (and sometimes unreadable files) into dist/.
const publicSkip = new Set(['results'])

// copyPublicFiltered reproduces Vite's default public-dir copy, minus the
// entries listed in publicSkip. Used together with `build.copyPublicDir:
// false` to opt out of the built-in recursive copy.
function copyPublicFiltered(): Plugin {
  return {
    name: 'benchmarkoor:copy-public-filtered',
    apply: 'build',
    closeBundle() {
      const src = resolve(__dirname, 'public')
      const dst = resolve(__dirname, 'dist')

      for (const entry of readdirSync(src, { withFileTypes: true })) {
        if (publicSkip.has(entry.name)) continue

        cpSync(join(src, entry.name), join(dst, entry.name), {
          recursive: true,
          // Copy the target of a symlink, not the link itself, to match
          // Vite's default behaviour for the other static assets.
          dereference: true,
        })
      }
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), copyPublicFiltered()],
  base: '/',
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    // Disabled so the filtered copy above is the single source of truth.
    copyPublicDir: false,
  },
  server: {
    watch: {
      ignored: ['**/results/**'],
    },
  },
})
