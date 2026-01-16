import { createRouter, createRootRoute, createRoute, redirect, Outlet } from '@tanstack/react-router'
import { Header } from '@/components/layout/Header'
import { Footer } from '@/components/layout/Footer'
import { RunsPage } from '@/pages/RunsPage'
import { RunDetailPage } from '@/pages/RunDetailPage'
import { SuitesPage } from '@/pages/SuitesPage'
import { SuiteDetailPage } from '@/pages/SuiteDetailPage'

const rootRoute = createRootRoute({
  component: () => (
    <div className="flex min-h-dvh flex-col bg-gray-50 dark:bg-gray-900">
      <Header />
      <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-8">
        <Outlet />
      </main>
      <Footer />
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

const suitesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/suites',
  component: SuitesPage,
})

const suiteDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/suites/$suiteHash',
  component: SuiteDetailPage,
})

const routeTree = rootRoute.addChildren([indexRoute, runsRoute, runDetailRoute, suitesRoute, suiteDetailRoute])

export const router = createRouter({ routeTree })
