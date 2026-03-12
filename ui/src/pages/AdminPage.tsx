import { useMemo, useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useAuth } from '@/hooks/useAuth'
import type { AuthConfig } from '@/api/auth-client'
import type { AuthUser } from '@/api/auth-client'
import { Modal } from '@/components/shared/Modal'
import {
  useUsers,
  useCreateUser,
  useUpdateUser,
  useDeleteUser,
  useSessions,
  useDeleteSession,
  useOrgMappings,
  useUpsertOrgMapping,
  useDeleteOrgMapping,
  useUserMappings,
  useUpsertUserMapping,
  useDeleteUserMapping,
  useRunIndexer,
  type AdminSession,
} from '@/api/hooks/useAdmin'
import { useAdminApiKeys, useDeleteAdminApiKey, type AdminApiKey } from '@/api/hooks/useApiKeys'
import { Plus, Pencil, Trash2, Play, Loader2, ChevronUp, ChevronDown } from 'lucide-react'
import clsx from 'clsx'

type SortDirection = 'asc' | 'desc'

function useSortState<T extends string>(defaultColumn: T, defaultDir: SortDirection = 'asc') {
  const [sortBy, setSortBy] = useState<T>(defaultColumn)
  const [sortDir, setSortDir] = useState<SortDirection>(defaultDir)

  const toggle = (column: T) => {
    if (sortBy === column) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortBy(column)
      setSortDir('asc')
    }
  }

  return { sortBy, sortDir, toggle }
}

function SortIcon({ direction, active }: { direction: SortDirection; active: boolean }) {
  const Icon = direction === 'asc' ? ChevronUp : ChevronDown
  return <Icon className={clsx('ml-0.5 inline-block size-3', active ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400')} />
}

function SortableTh<T extends string>({
  label,
  column,
  sortBy,
  sortDir,
  onSort,
  className,
}: {
  label: string
  column: T
  sortBy: T
  sortDir: SortDirection
  onSort: (column: T) => void
  className?: string
}) {
  const active = sortBy === column
  return (
    <th
      onClick={() => onSort(column)}
      className={clsx(
        'cursor-pointer select-none px-4 py-2 hover:text-gray-700 dark:hover:text-gray-300',
        className,
      )}
    >
      {label}
      <SortIcon direction={active ? sortDir : 'asc'} active={active} />
    </th>
  )
}

function sortItems<T>(items: T[], sortBy: string, sortDir: SortDirection, comparators: Record<string, (a: T, b: T) => number>): T[] {
  const cmp = comparators[sortBy]
  if (!cmp) return items
  const sorted = [...items].sort(cmp)
  return sortDir === 'desc' ? sorted.reverse() : sorted
}

const cmpStr = (a: string, b: string) => a.localeCompare(b)
const cmpDate = (a: string, b: string) => new Date(a).getTime() - new Date(b).getTime()
const cmpDateNullable = (a: string | null, b: string | null) => {
  if (!a && !b) return 0
  if (!a) return -1
  if (!b) return 1
  return cmpDate(a, b)
}

type Tab = 'users' | 'github-mappings' | 'sessions' | 'api-keys'

export function AdminPage() {
  const { isAdmin, authConfig } = useAuth()

  const tabs = useMemo(() => {
    const result: { key: Tab; label: string }[] = []
    if (authConfig?.auth.basic_enabled) result.push({ key: 'users', label: 'Users' })
    if (authConfig?.auth.github_enabled) result.push({ key: 'github-mappings', label: 'GitHub Mappings' })
    result.push({ key: 'sessions', label: 'Sessions' })
    result.push({ key: 'api-keys', label: 'API Keys' })
    return result
  }, [authConfig])

  const navigate = useNavigate()
  const search = useSearch({ from: '/admin' }) as { tab?: string }
  const activeTab = (search.tab as Tab) || tabs[0]?.key
  const resolvedTab = tabs.find((t) => t.key === activeTab) ? activeTab : tabs[0]?.key

  const setActiveTab = (tab: Tab) => {
    navigate({ to: '/admin', search: { tab } })
  }

  if (!isAdmin) {
    return (
      <div className="py-12 text-center text-gray-500 dark:text-gray-400">
        You do not have permission to access this page.
      </div>
    )
  }

  return (
    <div>
      <h1 className="mb-6 text-xl font-semibold text-gray-900 dark:text-gray-100">Admin</h1>

      {authConfig && <ConfigOverview config={authConfig} />}

      {tabs.length > 0 && (
        <>
          <div className="mb-6 flex gap-1 border-b border-gray-200 dark:border-gray-700">
            {tabs.map((tab) => (
              <button
                key={tab.key}
                onClick={() => setActiveTab(tab.key)}
                className={clsx(
                  'border-b-2 px-4 py-2 text-sm font-medium transition-colors',
                  resolvedTab === tab.key
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200',
                )}
              >
                {tab.label}
              </button>
            ))}
          </div>

          {resolvedTab === 'users' && <UsersTab />}
          {resolvedTab === 'github-mappings' && <GitHubMappingsTab />}
          {resolvedTab === 'sessions' && <SessionsTab />}
          {resolvedTab === 'api-keys' && <AdminAPIKeysTab />}
        </>
      )}
    </div>
  )
}

// --- Users Tab ---

type UserSortColumn = 'username' | 'role' | 'source'

const userComparators: Record<UserSortColumn, (a: AuthUser, b: AuthUser) => number> = {
  username: (a, b) => cmpStr(a.username, b.username),
  role: (a, b) => cmpStr(a.role, b.role),
  source: (a, b) => cmpStr(a.source, b.source),
}

function UsersTab() {
  const { data: users = [], isLoading } = useUsers()
  const createUser = useCreateUser()
  const updateUser = useUpdateUser()
  const deleteUser = useDeleteUser()
  const { user: currentUser } = useAuth()
  const [showCreate, setShowCreate] = useState(false)
  const [editUser, setEditUser] = useState<{ id: number; username: string; role: string } | null>(null)

  const [form, setForm] = useState({ username: '', password: '', role: 'readonly' })
  const [editForm, setEditForm] = useState({ password: '', role: '' })
  const [error, setError] = useState<string | null>(null)
  const sort = useSortState<UserSortColumn>('username')

  const sortedUsers = useMemo(
    () => sortItems(users, sort.sortBy, sort.sortDir, userComparators),
    [users, sort.sortBy, sort.sortDir],
  )

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    try {
      await createUser.mutateAsync(form)
      setShowCreate(false)
      setForm({ username: '', password: '', role: 'readonly' })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create user')
    }
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editUser) return
    setError(null)
    try {
      const data: { id: number; password?: string; role?: string } = { id: editUser.id }
      if (editForm.password) data.password = editForm.password
      if (editForm.role && editForm.role !== editUser.role) data.role = editForm.role
      await updateUser.mutateAsync(data)
      setEditUser(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update user')
    }
  }

  if (isLoading) return <div className="text-sm text-gray-500">Loading...</div>

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {users.length} user{users.length !== 1 ? 's' : ''}
        </h2>
        <button
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-1.5 rounded-sm bg-gray-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200"
        >
          <Plus className="size-3.5" />
          Create User
        </button>
      </div>

      <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
        <table className="w-full text-left text-sm">
          <thead className="bg-gray-50 text-xs text-gray-500 uppercase dark:bg-gray-800 dark:text-gray-400">
            <tr>
              <SortableTh label="Username" column="username" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Role" column="role" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Source" column="source" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {sortedUsers.map((u) => (
              <tr key={u.id} className="bg-white dark:bg-gray-900">
                <td className="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">{u.username}</td>
                <td className="px-4 py-2">
                  <span
                    className={clsx(
                      'inline-block rounded-full px-2 py-0.5 text-xs font-medium',
                      u.role === 'admin'
                        ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                        : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
                    )}
                  >
                    {u.role}
                  </span>
                </td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">{u.source}</td>
                <td className="px-4 py-2 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <button
                      onClick={() => {
                        setEditUser({ id: u.id, username: u.username, role: u.role })
                        setEditForm({ password: '', role: u.role })
                        setError(null)
                      }}
                      className="rounded-sm p-1 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"
                      title="Edit"
                    >
                      <Pencil className="size-3.5" />
                    </button>
                    {currentUser?.id !== u.id && (
                      <button
                        onClick={() => {
                          if (confirm(`Delete user "${u.username}"?`)) {
                            deleteUser.mutate(u.id)
                          }
                        }}
                        className="rounded-sm p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                        title="Delete"
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Modal isOpen={showCreate} onClose={() => setShowCreate(false)} title="Create User">
        <form onSubmit={handleCreate} className="space-y-4">
          {error && <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{error}</div>}
          <InputField label="Username" value={form.username} onChange={(v) => setForm({ ...form, username: v })} required />
          <InputField label="Password" value={form.password} onChange={(v) => setForm({ ...form, password: v })} required type="password" />
          <RoleSelect value={form.role} onChange={(v) => setForm({ ...form, role: v })} />
          <button type="submit" className="w-full rounded-sm bg-gray-800 px-4 py-2 text-sm font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200">
            Create
          </button>
        </form>
      </Modal>

      <Modal isOpen={!!editUser} onClose={() => setEditUser(null)} title={`Edit ${editUser?.username ?? ''}`}>
        <form onSubmit={handleUpdate} className="space-y-4">
          {error && <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{error}</div>}
          <InputField label="New Password (leave blank to keep)" value={editForm.password} onChange={(v) => setEditForm({ ...editForm, password: v })} type="password" />
          <RoleSelect value={editForm.role} onChange={(v) => setEditForm({ ...editForm, role: v })} />
          <button type="submit" className="w-full rounded-sm bg-gray-800 px-4 py-2 text-sm font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200">
            Save
          </button>
        </form>
      </Modal>
    </div>
  )
}

// --- Sessions Tab ---

function formatTimestamp(iso: string): string {
  const date = new Date(iso)
  const now = new Date()
  const diffMs = date.getTime() - now.getTime()
  const absDiffMs = Math.abs(diffMs)
  const isPast = diffMs < 0

  const minutes = Math.floor(absDiffMs / 60000)
  const hours = Math.floor(minutes / 60)
  const days = Math.floor(hours / 24)

  let relative: string
  if (minutes < 1) relative = 'just now'
  else if (minutes < 60) relative = `${minutes}m ${isPast ? 'ago' : 'from now'}`
  else if (hours < 24) relative = `${hours}h ${isPast ? 'ago' : 'from now'}`
  else relative = `${days}d ${isPast ? 'ago' : 'from now'}`

  return `${relative} (${date.toLocaleString()})`
}

type SessionSortColumn = 'username' | 'source' | 'created' | 'lastActive' | 'expires'

const sessionComparators: Record<SessionSortColumn, (a: AdminSession, b: AdminSession) => number> = {
  username: (a, b) => cmpStr(a.username || '', b.username || ''),
  source: (a, b) => cmpStr(a.source, b.source),
  created: (a, b) => cmpDate(a.created_at, b.created_at),
  lastActive: (a, b) => cmpDateNullable(a.last_active_at, b.last_active_at),
  expires: (a, b) => cmpDate(a.expires_at, b.expires_at),
}

function SessionsTab() {
  const { data: sessions = [], isLoading } = useSessions()
  const deleteSession = useDeleteSession()
  const sort = useSortState<SessionSortColumn>('created', 'desc')

  const sortedSessions = useMemo(
    () => sortItems(sessions, sort.sortBy, sort.sortDir, sessionComparators),
    [sessions, sort.sortBy, sort.sortDir],
  )

  if (isLoading) return <div className="text-sm text-gray-500">Loading...</div>

  return (
    <div>
      <div className="mb-4">
        <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {sessions.length} session{sessions.length !== 1 ? 's' : ''}
        </h2>
      </div>

      <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
        <table className="w-full text-left text-sm">
          <thead className="bg-gray-50 text-xs text-gray-500 uppercase dark:bg-gray-800 dark:text-gray-400">
            <tr>
              <SortableTh label="Username" column="username" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Source" column="source" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Created" column="created" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Last Active" column="lastActive" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Expires" column="expires" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {sortedSessions.map((s) => (
              <tr key={s.id} className="bg-white dark:bg-gray-900">
                <td className="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">
                  {s.username || `User #${s.user_id}`}
                </td>
                <td className="px-4 py-2">
                  <span
                    className={clsx(
                      'inline-block rounded-full px-2 py-0.5 text-xs font-medium',
                      s.source === 'github'
                        ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                        : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
                    )}
                  >
                    {s.source}
                  </span>
                </td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                  {formatTimestamp(s.created_at)}
                </td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                  {s.last_active_at ? formatTimestamp(s.last_active_at) : 'Never'}
                </td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                  {formatTimestamp(s.expires_at)}
                </td>
                <td className="px-4 py-2 text-right">
                  <button
                    onClick={() => {
                      if (confirm(`Revoke session for "${s.username || `User #${s.user_id}`}"?`)) {
                        deleteSession.mutate(s.id)
                      }
                    }}
                    className="rounded-sm p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                    title="Revoke session"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </td>
              </tr>
            ))}
            {sessions.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
                  No active sessions
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// --- GitHub Mappings Tab ---

function GitHubMappingsTab() {
  const { data: orgMappings = [], isLoading: orgLoading } = useOrgMappings()
  const { data: userMappings = [], isLoading: userLoading } = useUserMappings()
  const upsertOrg = useUpsertOrgMapping()
  const removeOrg = useDeleteOrgMapping()
  const upsertUser = useUpsertUserMapping()
  const removeUser = useDeleteUserMapping()

  const [showAddOrg, setShowAddOrg] = useState(false)
  const [orgForm, setOrgForm] = useState({ org: '', role: 'readonly' })
  const [orgError, setOrgError] = useState<string | null>(null)

  const [showAddUser, setShowAddUser] = useState(false)
  const [userForm, setUserForm] = useState({ username: '', role: 'readonly' })
  const [userError, setUserError] = useState<string | null>(null)

  const handleOrgSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setOrgError(null)
    try {
      await upsertOrg.mutateAsync(orgForm)
      setShowAddOrg(false)
      setOrgForm({ org: '', role: 'readonly' })
    } catch (err) {
      setOrgError(err instanceof Error ? err.message : 'Failed to save mapping')
    }
  }

  const handleUserSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setUserError(null)
    try {
      await upsertUser.mutateAsync(userForm)
      setShowAddUser(false)
      setUserForm({ username: '', role: 'readonly' })
    } catch (err) {
      setUserError(err instanceof Error ? err.message : 'Failed to save mapping')
    }
  }

  if (orgLoading || userLoading) return <div className="text-sm text-gray-500">Loading...</div>

  return (
    <div className="space-y-8">
      {/* Org Mappings */}
      <section>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Org Mappings ({orgMappings.length})
          </h2>
          <button
            onClick={() => setShowAddOrg(true)}
            className="flex items-center gap-1.5 rounded-sm bg-gray-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200"
          >
            <Plus className="size-3.5" />
            Add Mapping
          </button>
        </div>

        <MappingTable
          items={orgMappings}
          nameKey="org"
          nameLabel="Organization"
          onDelete={(id) => {
            if (confirm('Delete this mapping?')) removeOrg.mutate(id)
          }}
        />

        <Modal isOpen={showAddOrg} onClose={() => setShowAddOrg(false)} title="Add Org Mapping">
          <form onSubmit={handleOrgSubmit} className="space-y-4">
            {orgError && <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{orgError}</div>}
            <InputField label="Organization" value={orgForm.org} onChange={(v) => setOrgForm({ ...orgForm, org: v })} required />
            <RoleSelect value={orgForm.role} onChange={(v) => setOrgForm({ ...orgForm, role: v })} />
            <button type="submit" className="w-full rounded-sm bg-gray-800 px-4 py-2 text-sm font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200">
              Save
            </button>
          </form>
        </Modal>
      </section>

      {/* User Mappings */}
      <section>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
            User Mappings ({userMappings.length})
          </h2>
          <button
            onClick={() => setShowAddUser(true)}
            className="flex items-center gap-1.5 rounded-sm bg-gray-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200"
          >
            <Plus className="size-3.5" />
            Add Mapping
          </button>
        </div>

        <MappingTable
          items={userMappings}
          nameKey="username"
          nameLabel="Username"
          onDelete={(id) => {
            if (confirm('Delete this mapping?')) removeUser.mutate(id)
          }}
        />

        <Modal isOpen={showAddUser} onClose={() => setShowAddUser(false)} title="Add User Mapping">
          <form onSubmit={handleUserSubmit} className="space-y-4">
            {userError && <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{userError}</div>}
            <InputField label="GitHub Username" value={userForm.username} onChange={(v) => setUserForm({ ...userForm, username: v })} required />
            <RoleSelect value={userForm.role} onChange={(v) => setUserForm({ ...userForm, role: v })} />
            <button type="submit" className="w-full rounded-sm bg-gray-800 px-4 py-2 text-sm font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200">
              Save
            </button>
          </form>
        </Modal>
      </section>
    </div>
  )
}

// --- Admin API Keys Tab ---

type ApiKeySortColumn = 'name' | 'user' | 'expires' | 'lastUsed' | 'created'

const apiKeyComparators: Record<ApiKeySortColumn, (a: AdminApiKey, b: AdminApiKey) => number> = {
  name: (a, b) => cmpStr(a.name, b.name),
  user: (a, b) => cmpStr(a.username, b.username),
  expires: (a, b) => cmpDateNullable(a.expires_at, b.expires_at),
  lastUsed: (a, b) => cmpDateNullable(a.last_used_at, b.last_used_at),
  created: (a, b) => cmpDate(a.created_at, b.created_at),
}

function AdminAPIKeysTab() {
  const { data: keys = [], isLoading } = useAdminApiKeys()
  const deleteKey = useDeleteAdminApiKey()
  const sort = useSortState<ApiKeySortColumn>('created', 'desc')

  const sortedKeys = useMemo(
    () => sortItems(keys, sort.sortBy, sort.sortDir, apiKeyComparators),
    [keys, sort.sortBy, sort.sortDir],
  )

  if (isLoading) return <div className="text-sm text-gray-500">Loading...</div>

  return (
    <div>
      <div className="mb-4">
        <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {keys.length} key{keys.length !== 1 ? 's' : ''}
        </h2>
      </div>

      <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
        <table className="w-full text-left text-sm">
          <thead className="bg-gray-50 text-xs text-gray-500 uppercase dark:bg-gray-800 dark:text-gray-400">
            <tr>
              <SortableTh label="Name" column="name" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="User" column="user" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <th className="px-4 py-2">Key</th>
              <SortableTh label="Expires" column="expires" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Last Used" column="lastUsed" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <SortableTh label="Created" column="created" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {sortedKeys.map((k) => (
              <tr key={k.id} className="bg-white dark:bg-gray-900">
                <td className="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">{k.name}</td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">{k.username}</td>
                <td className="px-4 py-2">
                  <code className="rounded-xs bg-gray-100 px-1.5 py-0.5 text-xs font-mono text-gray-600 dark:bg-gray-800 dark:text-gray-400">
                    bmk_{k.key_prefix}...
                  </code>
                </td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                  {k.expires_at ? formatTimestamp(k.expires_at) : 'Never'}
                </td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                  {k.last_used_at ? formatTimestamp(k.last_used_at) : 'Never'}
                </td>
                <td className="px-4 py-2 text-gray-500 dark:text-gray-400">
                  {formatTimestamp(k.created_at)}
                </td>
                <td className="px-4 py-2 text-right">
                  <button
                    onClick={() => {
                      if (confirm(`Delete API key "${k.name}" (user: ${k.username})?`)) {
                        deleteKey.mutate(k.id)
                      }
                    }}
                    className="rounded-sm p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                    title="Delete"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </td>
              </tr>
            ))}
            {keys.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
                  No API keys
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// --- Config Overview ---

function ConfigOverview({ config }: { config: AuthConfig }) {
  const s3 = config.storage?.s3
  const local = config.storage?.local
  const indexingEnabled = config.indexing?.enabled ?? false

  const runIndexer = useRunIndexer()
  const [indexerMessage, setIndexerMessage] = useState<{ text: string; isError: boolean } | null>(null)

  const handleRunIndexer = async () => {
    setIndexerMessage(null)
    try {
      const result = await runIndexer.mutateAsync()
      setIndexerMessage({ text: result.message, isError: false })
    } catch (err) {
      setIndexerMessage({
        text: err instanceof Error ? err.message : 'Failed to trigger indexer',
        isError: true,
      })
    }
  }

  const items: { label: string; enabled: boolean }[] = [
    { label: 'Basic Auth', enabled: config.auth.basic_enabled },
    { label: 'GitHub Auth', enabled: config.auth.github_enabled },
    { label: 'S3 Storage', enabled: s3?.enabled ?? false },
    { label: 'Local Storage', enabled: local?.enabled ?? false },
    { label: 'Indexing', enabled: indexingEnabled },
  ]

  return (
    <div className="mb-6 rounded-sm border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800/50">
      <h2 className="mb-2 text-xs font-medium text-gray-500 uppercase dark:text-gray-400">Config</h2>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-1 text-sm">
        {items.map((item) => (
          <span key={item.label} className="flex items-center gap-1.5">
            <span
              className={clsx(
                'inline-block size-2 rounded-full',
                item.enabled ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600',
              )}
            />
            <span className="text-gray-700 dark:text-gray-300">{item.label}</span>
          </span>
        ))}
        {indexingEnabled && (
          <button
            onClick={handleRunIndexer}
            disabled={runIndexer.isPending}
            className="flex items-center gap-1 rounded-sm border border-gray-300 px-2 py-0.5 text-xs font-medium text-gray-700 hover:bg-gray-200 disabled:opacity-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
          >
            {runIndexer.isPending ? (
              <Loader2 className="size-3 animate-spin" />
            ) : (
              <Play className="size-3" />
            )}
            Run Indexer
          </button>
        )}
      </div>
      {indexerMessage && (
        <div
          className={clsx(
            'mt-2 text-xs',
            indexerMessage.isError
              ? 'text-red-600 dark:text-red-400'
              : 'text-green-600 dark:text-green-400',
          )}
        >
          {indexerMessage.text}
        </div>
      )}
      {s3?.enabled && s3.discovery_paths.length > 0 && (
        <div className="mt-2 text-xs text-gray-500 dark:text-gray-400">
          S3 discovery paths: {s3.discovery_paths.join(', ')}
        </div>
      )}
      {local?.enabled && local.discovery_paths.length > 0 && (
        <div className="mt-2 text-xs text-gray-500 dark:text-gray-400">
          Local discovery paths: {local.discovery_paths.join(', ')}
        </div>
      )}
    </div>
  )
}

// --- Shared Components ---

function InputField({
  label,
  value,
  onChange,
  type = 'text',
  required = false,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  type?: string
  required?: boolean
}) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required={required}
        className="mt-1 block w-full rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
      />
    </div>
  )
}

function RoleSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">Role</label>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="mt-1 block w-full rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
      >
        <option value="readonly">readonly</option>
        <option value="admin">admin</option>
      </select>
    </div>
  )
}

type MappingSortColumn = 'name' | 'role'

function MappingTable<T extends { id: number; role: string }>({
  items,
  nameKey,
  nameLabel,
  onDelete,
}: {
  items: T[]
  nameKey: keyof T
  nameLabel: string
  onDelete: (id: number) => void
}) {
  const sort = useSortState<MappingSortColumn>('name')

  const sortedItems = useMemo(
    () => sortItems(items, sort.sortBy, sort.sortDir, {
      name: (a, b) => String(a[nameKey]).localeCompare(String(b[nameKey])),
      role: (a, b) => a.role.localeCompare(b.role),
    } as Record<string, (a: T, b: T) => number>),
    [items, sort.sortBy, sort.sortDir, nameKey],
  )

  return (
    <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
      <table className="w-full text-left text-sm">
        <thead className="bg-gray-50 text-xs text-gray-500 uppercase dark:bg-gray-800 dark:text-gray-400">
          <tr>
            <SortableTh label={nameLabel} column="name" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
            <SortableTh label="Role" column="role" sortBy={sort.sortBy} sortDir={sort.sortDir} onSort={sort.toggle} />
            <th className="px-4 py-2 text-right">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {sortedItems.map((item) => (
            <tr key={item.id} className="bg-white dark:bg-gray-900">
              <td className="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">
                {String(item[nameKey])}
              </td>
              <td className="px-4 py-2">
                <span
                  className={clsx(
                    'inline-block rounded-full px-2 py-0.5 text-xs font-medium',
                    item.role === 'admin'
                      ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                      : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
                  )}
                >
                  {item.role}
                </span>
              </td>
              <td className="px-4 py-2 text-right">
                <button
                  onClick={() => onDelete(item.id)}
                  className="rounded-sm p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                  title="Delete"
                >
                  <Trash2 className="size-3.5" />
                </button>
              </td>
            </tr>
          ))}
          {items.length === 0 && (
            <tr>
              <td colSpan={3} className="px-4 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
                No mappings configured
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
