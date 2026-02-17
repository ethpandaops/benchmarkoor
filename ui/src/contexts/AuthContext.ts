import { createContext } from 'react'
import type { AuthConfig, AuthUser } from '@/api/auth-client'

export interface AuthContextValue {
  user: AuthUser | null
  isLoading: boolean
  isApiEnabled: boolean
  authConfig: AuthConfig | null
  requiresLogin: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  isAdmin: boolean
}

export const AuthContext = createContext<AuthContextValue>({
  user: null,
  isLoading: true,
  isApiEnabled: false,
  authConfig: null,
  requiresLogin: false,
  login: async () => {},
  logout: async () => {},
  isAdmin: false,
})
