import { useState } from 'react'
import { useAuth } from '@/hooks/useAuth'
import { useMyApiKeys, useCreateApiKey, useDeleteMyApiKey } from '@/api/hooks/useApiKeys'
import type { CreateApiKeyResponse } from '@/api/hooks/useApiKeys'
import { Modal } from '@/components/shared/Modal'
import { Plus, Trash2, Copy, Check, Key, AlertTriangle } from 'lucide-react'

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

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="rounded-sm p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
    </button>
  )
}

export function ApiKeysPage() {
  const { user } = useAuth()
  const { data: keys = [], isLoading } = useMyApiKeys()
  const createKey = useCreateApiKey()
  const deleteKey = useDeleteMyApiKey()

  const [showCreate, setShowCreate] = useState(false)
  const [createdKey, setCreatedKey] = useState<CreateApiKeyResponse | null>(null)
  const [name, setName] = useState('')
  const [expiresIn, setExpiresIn] = useState('')
  const [error, setError] = useState<string | null>(null)

  if (!user) {
    return (
      <div className="py-12 text-center text-gray-500 dark:text-gray-400">
        You must be signed in to manage API keys.
      </div>
    )
  }

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    const payload: { name: string; expires_at?: string } = { name }
    if (expiresIn) {
      const days = parseInt(expiresIn, 10)
      if (days > 0) {
        const expDate = new Date()
        expDate.setDate(expDate.getDate() + days)
        payload.expires_at = expDate.toISOString()
      }
    }

    try {
      const result = await createKey.mutateAsync(payload)
      setCreatedKey(result)
      setShowCreate(false)
      setName('')
      setExpiresIn('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create API key')
    }
  }

  return (
    <div>
      <h1 className="mb-6 text-xl font-semibold text-gray-900 dark:text-gray-100">API Keys</h1>

      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">
          {keys.length} key{keys.length !== 1 ? 's' : ''}
        </h2>
        <button
          onClick={() => { setShowCreate(true); setError(null) }}
          className="flex items-center gap-1.5 rounded-sm bg-gray-800 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200"
        >
          <Plus className="size-3.5" />
          Create API Key
        </button>
      </div>

      {isLoading ? (
        <div className="text-sm text-gray-500">Loading...</div>
      ) : (
        <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
          <table className="w-full text-left text-sm">
            <thead className="bg-gray-50 text-xs text-gray-500 uppercase dark:bg-gray-800 dark:text-gray-400">
              <tr>
                <th className="px-4 py-2">Name</th>
                <th className="px-4 py-2">Key</th>
                <th className="px-4 py-2">Expires</th>
                <th className="px-4 py-2">Last Used</th>
                <th className="px-4 py-2">Created</th>
                <th className="px-4 py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
              {keys.map((k) => (
                <tr key={k.id} className="bg-white dark:bg-gray-900">
                  <td className="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">
                    <div className="flex items-center gap-1.5">
                      <Key className="size-3.5 text-gray-400" />
                      {k.name}
                    </div>
                  </td>
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
                        if (confirm(`Delete API key "${k.name}"?`)) {
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
                  <td colSpan={6} className="px-4 py-6 text-center text-sm text-gray-500 dark:text-gray-400">
                    No API keys. Create one to use in curl commands.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Create modal */}
      <Modal isOpen={showCreate} onClose={() => setShowCreate(false)} title="Create API Key">
        <form onSubmit={handleCreate} className="space-y-4">
          {error && (
            <div className="rounded-sm bg-red-50 p-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">
              {error}
            </div>
          )}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              placeholder="e.g. CI downloads"
              className="mt-1 block w-full rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">Expiration</label>
            <select
              value={expiresIn}
              onChange={(e) => setExpiresIn(e.target.value)}
              className="mt-1 block w-full rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 focus:border-blue-500 focus:ring-1 focus:ring-blue-500 focus:outline-none dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
            >
              <option value="">No expiration</option>
              <option value="7">7 days</option>
              <option value="30">30 days</option>
              <option value="90">90 days</option>
              <option value="365">1 year</option>
            </select>
          </div>
          <button
            type="submit"
            className="w-full rounded-sm bg-gray-800 px-4 py-2 text-sm font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200"
          >
            Create
          </button>
        </form>
      </Modal>

      {/* Show created key modal */}
      <Modal isOpen={!!createdKey} onClose={() => setCreatedKey(null)} title="API Key Created">
        {createdKey && (
          <div className="space-y-4">
            <div className="flex items-start gap-2 rounded-sm border border-amber-200 bg-amber-50 p-3 dark:border-amber-800 dark:bg-amber-900/20">
              <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
              <p className="text-sm text-amber-800 dark:text-amber-300">
                Copy this key now. You won't be able to see it again.
              </p>
            </div>
            <div className="flex items-center gap-2 rounded-sm bg-gray-100 p-3 dark:bg-gray-900">
              <code className="min-w-0 flex-1 break-all font-mono text-sm text-gray-900 dark:text-gray-100">
                {createdKey.key}
              </code>
              <CopyButton text={createdKey.key} />
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400">
              Use this key in curl commands:{' '}
              <code className="rounded-xs bg-gray-100 px-1 py-0.5 font-mono dark:bg-gray-900">
                -H 'Authorization: Bearer {'{'}YOUR_KEY{'}'}'
              </code>
            </p>
          </div>
        )}
      </Modal>
    </div>
  )
}
