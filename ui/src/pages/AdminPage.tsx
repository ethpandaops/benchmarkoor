import { useState } from 'react'
import { useAuth } from '@/contexts/auth'
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

type Tab = 'users' | 'org-mappings' | 'user-mappings'

export function AdminPage() {
  const { isAdmin } = useAuth()
  const [activeTab, setActiveTab] = useState<Tab>('users')

  if (!isAdmin) {
    return (
      <div className="py-12 text-center text-gray-500 dark:text-gray-400">
        You do not have permission to access this page.
      </div>
    )
  }

  const tabs: { key: Tab; label: string }[] = [
    { key: 'users', label: 'Users' },
    { key: 'org-mappings', label: 'Org Mappings' },
    { key: 'user-mappings', label: 'User Mappings' },
  ]

  return (
    <div>
      <h1 className="mb-6 text-xl font-semibold text-gray-900 dark:text-gray-100">Admin</h1>
      <div className="mb-6 flex gap-1 border-b border-gray-200 dark:border-gray-700">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            className={clsx(
              'border-b-2 px-4 py-2 text-sm font-medium transition-colors',
              activeTab === tab.key
                ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200',
            )}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {activeTab === 'users' && <UsersTab />}
      {activeTab === 'org-mappings' && <OrgMappingsTab />}
      {activeTab === 'user-mappings' && <UserMappingsTab />}
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

// --- Org Mappings Tab ---

function OrgMappingsTab() {
  const { data: mappings = [], isLoading } = useOrgMappings()
  const upsert = useUpsertOrgMapping()
  const remove = useDeleteOrgMapping()
  const [showAdd, setShowAdd] = useState(false)
  const [form, setForm] = useState({ org: '', role: 'readonly' })
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    try {
      await upsert.mutateAsync(form)
      setShowAdd(false)
      setForm({ org: '', role: 'readonly' })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save mapping')
    }
  }

  if (isLoading) return <div className="text-sm text-gray-500">Loading...</div>

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {mappings.length} mapping{mappings.length !== 1 ? 's' : ''}
        </h2>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center gap-1.5 rounded-sm bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600"
        >
          <Plus className="size-3.5" />
          Add Mapping
        </button>
      </div>

      <MappingTable
        items={mappings}
        nameKey="org"
        nameLabel="Organization"
        onDelete={(id) => {
          if (confirm('Delete this mapping?')) remove.mutate(id)
        }}
      />

      <Modal isOpen={showAdd} onClose={() => setShowAdd(false)} title="Add Org Mapping">
        <form onSubmit={handleSubmit} className="space-y-4">
          {error && <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{error}</div>}
          <InputField label="Organization" value={form.org} onChange={(v) => setForm({ ...form, org: v })} required />
          <RoleSelect value={form.role} onChange={(v) => setForm({ ...form, role: v })} />
          <button type="submit" className="w-full rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600">
            Save
          </button>
        </form>
      </Modal>
    </div>
  )
}

// --- User Mappings Tab ---

function UserMappingsTab() {
  const { data: mappings = [], isLoading } = useUserMappings()
  const upsert = useUpsertUserMapping()
  const remove = useDeleteUserMapping()
  const [showAdd, setShowAdd] = useState(false)
  const [form, setForm] = useState({ username: '', role: 'readonly' })
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    try {
      await upsert.mutateAsync(form)
      setShowAdd(false)
      setForm({ username: '', role: 'readonly' })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save mapping')
    }
  }

  if (isLoading) return <div className="text-sm text-gray-500">Loading...</div>

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {mappings.length} mapping{mappings.length !== 1 ? 's' : ''}
        </h2>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center gap-1.5 rounded-sm bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600"
        >
          <Plus className="size-3.5" />
          Add Mapping
        </button>
      </div>

      <MappingTable
        items={mappings}
        nameKey="username"
        nameLabel="Username"
        onDelete={(id) => {
          if (confirm('Delete this mapping?')) remove.mutate(id)
        }}
      />

      <Modal isOpen={showAdd} onClose={() => setShowAdd(false)} title="Add User Mapping">
        <form onSubmit={handleSubmit} className="space-y-4">
          {error && <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">{error}</div>}
          <InputField label="GitHub Username" value={form.username} onChange={(v) => setForm({ ...form, username: v })} required />
          <RoleSelect value={form.role} onChange={(v) => setForm({ ...form, role: v })} />
          <button type="submit" className="w-full rounded-sm bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600">
            Save
          </button>
        </form>
      </Modal>
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
