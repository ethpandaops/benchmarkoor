import { useCallback, useEffect, useState } from 'react'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { loadRuntimeConfig } from '@/config/runtime'
import {
  fetchAuthConfig,
  fetchMe,
  login as loginApi,
  logout as logoutApi,
} from '@/api/auth-client'
import { AuthContext } from '@/contexts/AuthContext'
import type { AuthContextValue } from '@/contexts/AuthContext'

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
      queryClient.invalidateQueries()
    },
  })

  const logoutMutation = useMutation({
    mutationFn: () => logoutApi(apiBaseUrl!),
    onSuccess: () => {
      queryClient.invalidateQueries()
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
    window.location.href = '/'
  }, [logoutMutation])

  const isLoading = !configLoaded || (isApiEnabled && isUserLoading)
  const requiresLogin =
    isApiEnabled && !user && authConfig !== null && !authConfig.auth.anonymous_read

  const value: AuthContextValue = {
    user,
    isLoading,
    isApiEnabled,
    authConfig,
    requiresLogin,
    login,
    logout,
    isAdmin: user?.role === 'admin',
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
