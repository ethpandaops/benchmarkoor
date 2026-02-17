export interface AuthConfig {
  auth: {
    basic_enabled: boolean
    github_enabled: boolean
    anonymous_read: boolean
  }
  storage?: {
    s3: {
      enabled: boolean
      discovery_paths: string[]
    }
  }
}

export interface AuthUser {
  id: number
  username: string
  role: string
  source: string
}

export async function fetchAuthConfig(baseUrl: string): Promise<AuthConfig | null> {
  try {
    const resp = await fetch(`${baseUrl}/api/v1/config`, {
      credentials: 'include',
    })
    if (!resp.ok) return null
    return await resp.json()
  } catch {
    return null
  }
}

export async function login(
  baseUrl: string,
  username: string,
  password: string,
): Promise<{ user: AuthUser }> {
  const resp = await fetch(`${baseUrl}/api/v1/auth/login`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({ error: 'Login failed' }))
    throw new Error(data.error || 'Login failed')
  }
  return resp.json()
}

export async function logout(baseUrl: string): Promise<void> {
  await fetch(`${baseUrl}/api/v1/auth/logout`, {
    method: 'POST',
    credentials: 'include',
  })
}

export async function fetchMe(baseUrl: string): Promise<AuthUser> {
  const resp = await fetch(`${baseUrl}/api/v1/auth/me`, {
    credentials: 'include',
  })
  if (!resp.ok) {
    throw new Error('Not authenticated')
  }
  return resp.json()
}

export function getGitHubAuthUrl(baseUrl: string): string {
  return `${baseUrl}/api/v1/auth/github`
}
