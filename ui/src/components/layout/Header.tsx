import { useState, useEffect } from 'react'
import { Link, useMatchRoute, useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import { Sun, Moon, LogIn, LogOut, Shield, User, Menu, X, FileText, Search, Tags, Code2, Check, Settings } from 'lucide-react'
import { useAuth } from '@/hooks/useAuth'
import { useNameDisplayMode, type NameDisplayMode } from '@/hooks/useNameDisplayMode'

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

type Theme = 'light' | 'dark'

const NAME_MODE_OPTIONS: { value: NameDisplayMode; label: string; icon: typeof Tags }[] = [
  { value: 'decomposed', label: 'Decomposed', icon: Tags },
  { value: 'raw', label: 'Raw', icon: Code2 },
]

const THEME_OPTIONS: { value: Theme; label: string; icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
]

function MenuOption<T extends string>({ active, onClick, icon: Icon, label }: {
  active: boolean
  onClick: () => void
  icon: typeof Tags
  label: string
  value: T
}) {
  return (
    <button
      onClick={onClick}
      className={clsx(
        'flex w-full items-center gap-2 px-3 py-2 text-left text-sm',
        active
          ? 'bg-gray-50 text-gray-900 dark:bg-gray-700/50 dark:text-gray-100'
          : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700/50',
      )}
    >
      <Icon className="size-3.5 shrink-0" />
      <span className="flex-1">{label}</span>
      {active && <Check className="size-3.5 text-blue-500" />}
    </button>
  )
}

function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-3 pb-1 pt-0.5 text-[10px]/4 font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">
      {children}
    </div>
  )
}

function SettingsMenu() {
  const { mode: nameMode, setMode: setNameMode } = useNameDisplayMode()
  const [open, setOpen] = useState(false)
  const [theme, setThemeState] = useState<Theme>(() => {
    if (typeof window === 'undefined') return 'light'
    return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
  })

  useEffect(() => {
    if (theme === 'dark') {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }
    localStorage.setItem('theme', theme)
  }, [theme])

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="rounded-sm p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
        title="Settings"
      >
        <Settings className="size-5" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute right-0 z-50 mt-1 w-44 rounded-sm border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-800">
            <SectionHeader>Test names</SectionHeader>
            {NAME_MODE_OPTIONS.map((opt) => (
              <MenuOption
                key={opt.value}
                value={opt.value}
                label={opt.label}
                icon={opt.icon}
                active={nameMode === opt.value}
                onClick={() => { setNameMode(opt.value); setOpen(false) }}
              />
            ))}
            <div className="my-1 border-t border-gray-200 dark:border-gray-700" />
            <SectionHeader>Theme</SectionHeader>
            {THEME_OPTIONS.map((opt) => (
              <MenuOption
                key={opt.value}
                value={opt.value}
                label={opt.label}
                icon={opt.icon}
                active={theme === opt.value}
                onClick={() => { setThemeState(opt.value); setOpen(false) }}
              />
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function UserMenu({ onNavigate }: { onNavigate?: () => void }) {
  const { user, isAdmin, logout } = useAuth()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)

  if (!user) return null

  const handleLogout = async () => {
    setOpen(false)
    await logout()
    onNavigate?.()
    navigate({ to: '/runs' })
  }

  const handleNavigate = (to: string) => {
    setOpen(false)
    onNavigate?.()
    navigate({ to })
  }

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1.5 rounded-sm px-2 py-1.5 text-sm text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700/50"
      >
        {user.source === 'github' ? (
          <img src={`https://github.com/${user.username}.png`} alt="" className="size-6 rounded-full" />
        ) : (
          <User className="size-4" />
        )}
        <span>{user.username}</span>
        {isAdmin && <Shield className="size-3 text-purple-500" />}
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute right-0 z-50 mt-1 w-44 rounded-sm border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-800">
            <button
              onClick={() => handleNavigate('/api-keys')}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700/50"
            >
              <Shield className="size-3.5" />
              API Keys
            </button>
            <button
              onClick={() => handleNavigate('/api-docs')}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700/50"
            >
              <FileText className="size-3.5" />
              API Docs
            </button>
            <button
              onClick={() => handleNavigate('/query')}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700/50"
            >
              <Search className="size-3.5" />
              Query Builder
            </button>
            {isAdmin && (
              <button
                onClick={() => handleNavigate('/admin')}
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700/50"
              >
                <User className="size-3.5" />
                Admin
              </button>
            )}
            <div className="my-1 border-t border-gray-200 dark:border-gray-700" />
            <button
              onClick={handleLogout}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700/50"
            >
              <LogOut className="size-3.5" />
              Sign out
            </button>
          </div>
        </>
      )}
    </div>
  )
}

function MobileUserMenu({ onNavigate }: { onNavigate?: () => void }) {
  const { user, isAdmin, logout } = useAuth()
  const navigate = useNavigate()

  if (!user) return null

  const handleLogout = async () => {
    await logout()
    onNavigate?.()
    navigate({ to: '/runs' })
  }

  const handleNavigate = (to: string) => {
    onNavigate?.()
    navigate({ to })
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2 px-3 py-1.5 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
        {user.source === 'github' ? (
          <img src={`https://github.com/${user.username}.png`} alt="" className="size-6 rounded-full" />
        ) : (
          <User className="size-4" />
        )}
        <span>{user.username}</span>
        {isAdmin && <Shield className="size-3 text-purple-500" />}
      </div>
      <div className="ml-5 flex flex-col gap-1 border-l border-gray-200 pl-3 dark:border-gray-700">
        <button
          onClick={() => handleNavigate('/api-keys')}
          className="flex items-center gap-2 rounded-sm px-3 py-1.5 text-left text-sm/6 text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700/50 dark:hover:text-gray-100"
        >
          <Shield className="size-3.5" />
          API Keys
        </button>
        <button
          onClick={() => handleNavigate('/api-docs')}
          className="flex items-center gap-2 rounded-sm px-3 py-1.5 text-left text-sm/6 text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700/50 dark:hover:text-gray-100"
        >
          <FileText className="size-3.5" />
          API Docs
        </button>
        <button
          onClick={() => handleNavigate('/query')}
          className="flex items-center gap-2 rounded-sm px-3 py-1.5 text-left text-sm/6 text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700/50 dark:hover:text-gray-100"
        >
          <Search className="size-3.5" />
          Query Builder
        </button>
        {isAdmin && (
          <button
            onClick={() => handleNavigate('/admin')}
            className="flex items-center gap-2 rounded-sm px-3 py-1.5 text-left text-sm/6 text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700/50 dark:hover:text-gray-100"
          >
            <User className="size-3.5" />
            Admin
          </button>
        )}
        <button
          onClick={handleLogout}
          className="flex items-center gap-2 rounded-sm px-3 py-1.5 text-left text-sm/6 text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700/50 dark:hover:text-gray-100"
        >
          <LogOut className="size-3.5" />
          Sign out
        </button>
      </div>
    </div>
  )
}

function AuthControls({ onNavigate, variant = 'desktop' }: { onNavigate?: () => void; variant?: 'desktop' | 'mobile' }) {
  const { user, isApiEnabled } = useAuth()

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

  if (variant === 'mobile') {
    return <MobileUserMenu onNavigate={onNavigate} />
  }

  return <UserMenu onNavigate={onNavigate} />
}

export function Header() {
  const [mobileOpen, setMobileOpen] = useState(false)

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
          <SettingsMenu />
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
          <div className="mt-3 border-t border-gray-200 pt-3 dark:border-gray-700">
            <AuthControls onNavigate={closeMobile} variant="mobile" />
            <div className="mt-2 flex items-center justify-end gap-2">
              <SettingsMenu />
            </div>
          </div>
        </div>
      )}
    </header>
  )
}
