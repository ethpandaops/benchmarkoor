import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryCache, MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { AuthProvider } from '@/contexts/auth'
import { loadRuntimeConfig } from '@/config/runtime'
import { reportApiDown, reportApiUp } from '@/api/api-status-events'
import { router } from './router'
import './index.css'

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// Forward GitHub OAuth callback params to the API before React mounts.
// When the GitHub OAuth redirect_url points to the UI, GitHub redirects
// here with ?code=...&state=... — we must forward to the API callback
// before the router's beforeLoad can strip the query params.
function handleOAuthCallback(): boolean {
  const params = new URLSearchParams(window.location.search)
  const code = params.get('code')
  const state = params.get('state')

  if (!code || !state) return false

  loadRuntimeConfig().then((cfg) => {
    if (cfg.api?.baseUrl) {
      window.location.href =
        `${cfg.api.baseUrl}/api/v1/auth/github/callback?code=${encodeURIComponent(code)}&state=${encodeURIComponent(state)}`
    }
  })

  return true
}

function isNetworkError(error: unknown): boolean {
  return error instanceof TypeError
}

if (!handleOAuthCallback()) {
  const queryCache = new QueryCache({
    onError: (error) => { if (isNetworkError(error)) reportApiDown() },
    onSuccess: () => reportApiUp(),
  })

  const mutationCache = new MutationCache({
    onError: (error) => { if (isNetworkError(error)) reportApiDown() },
    onSuccess: () => reportApiUp(),
  })

  const queryClient = new QueryClient({
    queryCache,
    mutationCache,
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
      },
    },
  })

  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <RouterProvider router={router} />
        </AuthProvider>
      </QueryClientProvider>
    </StrictMode>,
  )
}
