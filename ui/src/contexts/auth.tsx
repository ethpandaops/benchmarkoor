import { createContext, useContext, useCallback, useEffect, useState } from 'react'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { loadRuntimeConfig } from '@/config/runtime'
import {
  fetchAuthConfig,
  fetchMe,
  login as loginApi,
  logout as logoutApi,
  type AuthConfig,
  type AuthUser,
} from '@/api/auth-client'

interface AuthContextValue {
  user: AuthUser | null
  isLoading: boolean
  isApiEnabled: boolean
  authConfig: AuthConfig | null
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  isAdmin: boolean
}

const AuthContext = createContext<AuthContextValue>({
  user: null,
  isLoading: true,
  isApiEnabled: false,
  authConfig: null,
  login: async () => {},
  logout: async () => {},
  isAdmin: false,
})

export function useAuth() {
  return useContext(AuthContext)
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient()
  const [apiBaseUrl, setApiBaseUrl] = useState<string | null>(null)
  const [configLoaded, setConfigLoaded] = useState(false)

  useEffect(() => {
    loadRuntimeConfig().then((cfg) => {
      setApiBaseUrl(cfg.api?.baseUrl ?? null)
      setConfigLoaded(true)
    })
  }, [])

  const isApiEnabled = apiBaseUrl !== null

  const { data: authConfig = null } = useQuery({
    queryKey: ['authConfig'],
    queryFn: () => fetchAuthConfig(apiBaseUrl!),
    enabled: isApiEnabled,
    staleTime: 5 * 60 * 1000,
  })

  const { data: user = null, isLoading: isUserLoading } = useQuery({
    queryKey: ['authMe'],
    queryFn: () => fetchMe(apiBaseUrl!),
    enabled: isApiEnabled,
    retry: false,
    staleTime: 60 * 1000,
  })

  const loginMutation = useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) =>
      loginApi(apiBaseUrl!, username, password),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['authMe'] })
    },
  })

  const logoutMutation = useMutation({
    mutationFn: () => logoutApi(apiBaseUrl!),
    onSuccess: () => {
      queryClient.setQueryData(['authMe'], null)
    },
  })

  const login = useCallback(
    async (username: string, password: string) => {
      await loginMutation.mutateAsync({ username, password })
    },
    [loginMutation],
  )

  const logout = useCallback(async () => {
    await logoutMutation.mutateAsync()
  }, [logoutMutation])

  const isLoading = !configLoaded || (isApiEnabled && isUserLoading)

  const value: AuthContextValue = {
    user,
    isLoading,
    isApiEnabled,
    authConfig,
    login,
    logout,
    isAdmin: user?.role === 'admin',
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
