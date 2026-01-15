import { createRouter, createRootRoute, createRoute, redirect, Outlet } from '@tanstack/react-router'
import { Header } from '@/components/layout/Header'
import { RunsPage } from '@/pages/RunsPage'
import { RunDetailPage } from '@/pages/RunDetailPage'

const rootRoute = createRootRoute({
  component: () => (
    <div className="min-h-dvh bg-gray-50 dark:bg-gray-900">
      <Header />
      <main className="mx-auto max-w-7xl px-4 py-8">
        <Outlet />
      </main>
    </div>
  ),
})

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/runs' })
  },
})

const runsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runs',
  component: RunsPage,
})

const runDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runs/$runId',
  component: RunDetailPage,
})

const routeTree = rootRoute.addChildren([indexRoute, runsRoute, runDetailRoute])

export const router = createRouter({ routeTree })
