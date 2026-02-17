import { useEffect } from 'react'
import { Outlet, useNavigate, useLocation } from '@tanstack/react-router'
import { useAuth } from '@/hooks/useAuth'
import { Header } from '@/components/layout/Header'
import { Footer } from '@/components/layout/Footer'

export function RootLayout() {
  const { requiresLogin, isLoading } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()

  useEffect(() => {
    if (!isLoading && requiresLogin && location.pathname !== '/login') {
      navigate({ to: '/login' })
    }
  }, [isLoading, requiresLogin, location.pathname, navigate])

  return (
    <div className="flex min-h-dvh flex-col bg-gray-50 dark:bg-gray-900">
      {!requiresLogin && <Header />}
      <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-8">
        <Outlet />
      </main>
      <Footer />
    </div>
  )
}
