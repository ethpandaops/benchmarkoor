import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { readFileSync } from 'fs'
import { resolve } from 'path'

// Point the dev server at a remote API (for example prod) without editing
// public/config.json:
//
//   BENCHMARKOOR_API=https://benchmarkoor-api.core.ethpandaops.io \
//   BENCHMARKOOR_API_KEY=bmk_... npm run dev
//
// The dev server proxies /api to the remote host and adds the Bearer key on
// every proxied request (websockets included). The browser only ever talks to
// localhost, so no CORS and no cross-site cookie is involved, and the UI is
// authenticated as the owner of the key.
const remoteApi = process.env.BENCHMARKOOR_API
const remoteApiKey = process.env.BENCHMARKOOR_API_KEY

// remoteApiConfig serves /config.json with api.baseUrl set to the dev server
// itself, so every API call goes through the proxy above.
function remoteApiConfig(): Plugin {
  return {
    name: 'benchmarkoor-remote-api-config',
    apply: 'serve',
    configureServer(server) {
      if (!remoteApi) return

      server.middlewares.use((req, res, next) => {
        if (req.url?.split('?')[0] !== '/config.json') {
          next()
          return
        }

        const config = JSON.parse(
          readFileSync(resolve(__dirname, 'public/config.json'), 'utf8'),
        )
        config.api = { baseUrl: `http://${req.headers.host}` }

        res.setHeader('Content-Type', 'application/json')
        res.setHeader('Cache-Control', 'no-store')
        res.end(JSON.stringify(config))
      })
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), remoteApiConfig()],
  base: '/',
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
  },
  server: {
    watch: {
      ignored: ['**/results/**'],
    },
    proxy: remoteApi
      ? {
          '/api': {
            target: remoteApi,
            changeOrigin: true,
            ws: true,
            headers: remoteApiKey
              ? { Authorization: `Bearer ${remoteApiKey}` }
              : undefined,
          },
        }
      : undefined,
  },
})
