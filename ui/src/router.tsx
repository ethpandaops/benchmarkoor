import { createRouter, createRootRoute, createRoute, redirect, Outlet } from '@tanstack/react-router'
import { Header } from '@/components/layout/Header'
import { Footer } from '@/components/layout/Footer'
import { RunsPage } from '@/pages/RunsPage'
import { RunDetailPage } from '@/pages/RunDetailPage'
import { FileViewerPage } from '@/pages/FileViewerPage'
import { SuitesPage } from '@/pages/SuitesPage'
import { SuiteDetailPage } from '@/pages/SuiteDetailPage'
import { LoginPage } from '@/pages/LoginPage'
import { AdminPage } from '@/pages/AdminPage'

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

const fileViewerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runs/$runId/fileviewer',
  component: FileViewerPage,
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

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
})

const adminRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin',
  component: AdminPage,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  runsRoute,
  runDetailRoute,
  fileViewerRoute,
  suitesRoute,
  suiteDetailRoute,
  loginRoute,
  adminRoute,
])

export const router = createRouter({ routeTree })
