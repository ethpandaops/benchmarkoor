import { useState, useEffect } from 'react'
import { Link, useMatchRoute, useNavigate, useLocation } from '@tanstack/react-router'
import clsx from 'clsx'
import { Sun, Moon, LogIn, LogOut, Shield, User, Menu, X } from 'lucide-react'
import { useAuth } from '@/hooks/useAuth'

function NavLink({ to, children, onClick }: { to: string; children: React.ReactNode; onClick?: () => void }) {
  const matchRoute = useMatchRoute()
  const isActive = matchRoute({ to, fuzzy: true })

  return (
    <Link
      to={to}
      onClick={onClick}
      className={clsx(
        'rounded-sm px-3 py-1.5 text-sm/6 font-medium transition-colors',
        isActive
          ? 'bg-gray-100 text-gray-900 dark:bg-gray-700 dark:text-gray-100'
          : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700/50 dark:hover:text-gray-100',
      )}
    >
      {children}
    </Link>
  )
}

function ThemeSwitcher() {
  const [isDark, setIsDark] = useState(() => {
    if (typeof window === 'undefined') return false
    return document.documentElement.classList.contains('dark')
  })

  useEffect(() => {
    if (isDark) {
      document.documentElement.classList.add('dark')
      localStorage.setItem('theme', 'dark')
    } else {
      document.documentElement.classList.remove('dark')
      localStorage.setItem('theme', 'light')
    }
  }, [isDark])

  return (
    <button
      onClick={() => setIsDark(!isDark)}
      className="rounded-sm p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
      title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
    >
      {isDark ? <Sun className="size-5" /> : <Moon className="size-5" />}
    </button>
  )
}

function AuthControls({ onNavigate }: { onNavigate?: () => void }) {
  const { user, isApiEnabled, isAdmin, logout } = useAuth()
  const navigate = useNavigate()

  if (!isApiEnabled) return null

  if (!user) {
    return (
      <Link
        to="/login"
        onClick={onNavigate}
        className="flex items-center gap-1.5 rounded-sm px-3 py-1.5 text-sm font-medium text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700/50 dark:hover:text-gray-100"
      >
        <LogIn className="size-4" />
        Sign in
      </Link>
    )
  }

  const handleLogout = async () => {
    await logout()
    onNavigate?.()
    navigate({ to: '/runs' })
  }

  return (
    <div className="flex items-center gap-2">
      {isAdmin && <NavLink to="/admin" onClick={onNavigate}>Admin</NavLink>}
      <div className="flex items-center gap-1.5 text-sm text-gray-600 dark:text-gray-300">
        <User className="size-4" />
        <span>{user.username}</span>
        {isAdmin && <Shield className="size-3 text-purple-500" />}
      </div>
      <button
        onClick={handleLogout}
        className="flex items-center gap-1 rounded-sm p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
        title="Sign out"
      >
        <LogOut className="size-4" />
      </button>
    </div>
  )
}

export function Header() {
  const [mobileOpen, setMobileOpen] = useState(false)
  const location = useLocation()

  // Close mobile menu on route change.
  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

  const closeMobile = () => setMobileOpen(false)

  return (
    <header className="border-b border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <div className="mx-auto flex max-w-7xl items-center gap-8 px-4 py-2">
        <Link to="/runs" search={{}} className="flex items-center gap-2">
          <img src="/img/logo_black.png" alt="Benchmarkoor" className="h-12 dark:hidden" />
          <img src="/img/logo_white.png" alt="Benchmarkoor" className="hidden h-12 dark:block" />
          <span className="text-lg/7 font-semibold text-gray-900 dark:text-gray-100">Benchmarkoor</span>
        </Link>

        {/* Desktop nav */}
        <nav className="hidden items-center gap-1 md:flex">
          <NavLink to="/runs">Runs</NavLink>
          <NavLink to="/suites">Suites</NavLink>
        </nav>
        <div className="ml-auto hidden items-center gap-2 md:flex">
          <AuthControls />
          <ThemeSwitcher />
        </div>

        {/* Mobile hamburger */}
        <button
          onClick={() => setMobileOpen(!mobileOpen)}
          className="ml-auto rounded-sm p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200 md:hidden"
          aria-label="Toggle menu"
        >
          {mobileOpen ? <X className="size-5" /> : <Menu className="size-5" />}
        </button>
      </div>

      {/* Mobile menu */}
      {mobileOpen && (
        <div className="border-t border-gray-200 px-4 py-3 dark:border-gray-700 md:hidden">
          <nav className="flex flex-col gap-1">
            <NavLink to="/runs" onClick={closeMobile}>Runs</NavLink>
            <NavLink to="/suites" onClick={closeMobile}>Suites</NavLink>
          </nav>
          <div className="mt-3 flex items-center gap-2 border-t border-gray-200 pt-3 dark:border-gray-700">
            <AuthControls onNavigate={closeMobile} />
            <ThemeSwitcher />
          </div>
        </div>
      )}
    </header>
  )
}
