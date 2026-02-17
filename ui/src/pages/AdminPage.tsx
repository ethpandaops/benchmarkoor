import { useMemo, useState } from 'react'
import { useAuth } from '@/contexts/auth'
import type { AuthConfig } from '@/api/auth-client'
import { Modal } from '@/components/shared/Modal'
import {
  useUsers,
  useCreateUser,
  useUpdateUser,
  useDeleteUser,
  useOrgMappings,
  useUpsertOrgMapping,
  useDeleteOrgMapping,
  useUserMappings,
  useUpsertUserMapping,
  useDeleteUserMapping,
} from '@/api/hooks/useAdmin'
import { Plus, Pencil, Trash2 } from 'lucide-react'
import clsx from 'clsx'

type Tab = 'users' | 'github-mappings'

export function AdminPage() {
  const { isAdmin, authConfig } = useAuth()

  const tabs = useMemo(() => {
    const result: { key: Tab; label: string }[] = []
    if (authConfig?.auth.basic_enabled) result.push({ key: 'users', label: 'Users' })
    if (authConfig?.auth.github_enabled) result.push({ key: 'github-mappings', label: 'GitHub Mappings' })
    return result
  }, [authConfig])

  const [activeTab, setActiveTab] = useState<Tab>('users')
  const resolvedTab = tabs.find((t) => t.key === activeTab) ? activeTab : tabs[0]?.key

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
        </>
      )}
    </div>
  )
}

// --- Users Tab ---

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
          className="flex items-center gap-1.5 rounded-sm bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600"
        >
          <Plus className="size-3.5" />
          Create User
        </button>
      </div>

      <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
        <table className="w-full text-left text-sm">
          <thead className="bg-gray-50 text-xs text-gray-500 uppercase dark:bg-gray-800 dark:text-gray-400">
            <tr>
              <th className="px-4 py-2">Username</th>
              <th className="px-4 py-2">Role</th>
              <th className="px-4 py-2">Source</th>
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {users.map((u) => (
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
          <button type="submit" className="w-full rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600">
            Create
          </button>
        </form>
      </Modal>

      <Modal isOpen={!!editUser} onClose={() => setEditUser(null)} title={`Edit ${editUser?.username ?? ''}`}>
        <form onSubmit={handleUpdate} className="space-y-4">
          {error && <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{error}</div>}
          <InputField label="New Password (leave blank to keep)" value={editForm.password} onChange={(v) => setEditForm({ ...editForm, password: v })} type="password" />
          <RoleSelect value={editForm.role} onChange={(v) => setEditForm({ ...editForm, role: v })} />
          <button type="submit" className="w-full rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600">
            Save
          </button>
        </form>
      </Modal>
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
            className="flex items-center gap-1.5 rounded-sm bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600"
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
            <button type="submit" className="w-full rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600">
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
            className="flex items-center gap-1.5 rounded-sm bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600"
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
            <button type="submit" className="w-full rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600">
              Save
            </button>
          </form>
        </Modal>
      </section>
    </div>
  )
}

// --- Config Overview ---

function ConfigOverview({ config }: { config: AuthConfig }) {
  const s3 = config.storage?.s3

  const items: { label: string; enabled: boolean }[] = [
    { label: 'Basic Auth', enabled: config.auth.basic_enabled },
    { label: 'GitHub Auth', enabled: config.auth.github_enabled },
    { label: 'S3 Storage', enabled: s3?.enabled ?? false },
  ]

  return (
    <div className="mb-6 rounded-sm border border-gray-200 bg-gray-50 px-4 py-3 dark:border-gray-700 dark:bg-gray-800/50">
      <h2 className="mb-2 text-xs font-medium text-gray-500 uppercase dark:text-gray-400">Config</h2>
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-sm">
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
      </div>
      {s3?.enabled && s3.discovery_paths.length > 0 && (
        <div className="mt-2 text-xs text-gray-500 dark:text-gray-400">
          S3 discovery paths: {s3.discovery_paths.join(', ')}
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
  return (
    <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
      <table className="w-full text-left text-sm">
        <thead className="bg-gray-50 text-xs text-gray-500 uppercase dark:bg-gray-800 dark:text-gray-400">
          <tr>
            <th className="px-4 py-2">{nameLabel}</th>
            <th className="px-4 py-2">Role</th>
            <th className="px-4 py-2 text-right">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {items.map((item) => (
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
