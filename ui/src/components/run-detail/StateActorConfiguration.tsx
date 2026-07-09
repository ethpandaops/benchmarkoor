import { useState, type ReactNode } from 'react'

import { useQuery } from '@tanstack/react-query'
import clsx from 'clsx'
import { Check, Copy, Database, ExternalLink } from 'lucide-react'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'

import { fetchText } from '@/api/client'
import type { StateActorManifest } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Card } from '@/components/shared/Card'
import { getNavigableDataUrl, loadRuntimeConfig } from '@/config/runtime'
import { formatBytes, formatNumber } from '@/utils/format'

interface StateActorConfigurationProps {
  manifest: StateActorManifest
  runId: string
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
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
    </button>
  )
}

// compactNumber abbreviates large counts for compact header display
// (e.g. 300000000 -> "300M").
function compactNumber(n: number): string {
  if (n >= 1e9) return `${+(n / 1e9).toFixed(1)}B`
  if (n >= 1e6) return `${+(n / 1e6).toFixed(1)}M`
  if (n >= 1e3) return `${+(n / 1e3).toFixed(1)}K`

  return String(n)
}

// shortHash truncates a 0x hash to first-6…last-4 for inline display.
function shortHash(h: string): string {
  return h.length > 12 ? `${h.slice(0, 6)}…${h.slice(-4)}` : h
}

function langForFile(name: string): string {
  if (name.endsWith('.json')) return 'json'
  if (name.endsWith('.yaml') || name.endsWith('.yml')) return 'yaml'

  return 'text'
}

// RawFile shows a single .state-actor file (the manifest or a spec sidecar)
// with copy, open-raw and an expandable, syntax-highlighted raw-content view.
function RawFile({ runId, name }: { runId: string; name: string }) {
  const [open, setOpen] = useState(false)

  const { data: config } = useQuery({
    queryKey: ['runtime-config'],
    queryFn: loadRuntimeConfig,
    staleTime: Infinity,
  })

  const { data: file, isLoading } = useQuery({
    queryKey: ['run', runId, 'state-actor-file', name],
    queryFn: () => fetchText(`runs/${runId}/.state-actor/${name}`),
  })

  const text = file?.data ?? ''
  const url = config
    ? getNavigableDataUrl(`runs/${runId}/.state-actor/${name}`, config)
    : undefined

  return (
    <div className="overflow-hidden rounded-xs border border-gray-200 dark:border-gray-700">
      <div className="flex items-center justify-between gap-3 bg-gray-50 px-3 py-2 dark:bg-gray-900/40">
        <code className="min-w-0 truncate font-mono text-xs/5 text-gray-700 dark:text-gray-300">
          {name}
        </code>
        <div className="flex shrink-0 items-center gap-3">
          {text && <CopyButton text={text} />}
          {url && (
            <a
              href={url}
              target="_blank"
              rel="noreferrer"
              className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
              title="Open raw"
            >
              <ExternalLink className="size-4" />
            </a>
          )}
          <button
            onClick={() => setOpen(!open)}
            className="cursor-pointer text-xs/5 font-medium text-blue-600 hover:underline dark:text-blue-400"
          >
            {open ? 'Hide' : 'View'}
          </button>
        </div>
      </div>
      {open && (
        <SyntaxHighlighter
          language={langForFile(name)}
          style={oneDark}
          customStyle={{
            margin: 0,
            maxHeight: '24rem',
            borderRadius: 0,
            fontSize: '0.75rem',
          }}
        >
          {isLoading ? 'Loading…' : text || '(empty)'}
        </SyntaxHighlighter>
      )}
    </div>
  )
}

function Field({ label, value, mono }: { label: string; value: ReactNode; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">{label}</dt>
      <dd
        className={clsx(
          'mt-1 break-all text-sm/6 text-gray-900 dark:text-gray-100',
          mono && 'font-mono text-xs/5',
        )}
      >
        {value}
      </dd>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-3 border-t border-gray-200 pt-4 first:border-t-0 first:pt-0 dark:border-gray-700">
      <h4 className="text-xs/5 font-semibold tracking-wide text-gray-500 uppercase dark:text-gray-400">
        {title}
      </h4>
      <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">{children}</dl>
    </div>
  )
}

// StateActorConfiguration renders a run's state-actor provenance manifest
// (build, resolved flags, generation result, optional spec and command).
export function StateActorConfiguration({ manifest, runId }: StateActorConfigurationProps) {
  const { state_actor: sa, flags, result, spec } = manifest

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <Database className="size-4 text-gray-400 dark:text-gray-500" />
          Database
        </span>
      }
      headerExtra={
        <span className="hidden min-w-0 flex-wrap items-center gap-1.5 text-xs/5 sm:flex">
          {result && (
            <>
              <span className="text-gray-400 dark:text-gray-500">DB size:</span>
              <span className="text-gray-600 dark:text-gray-300">
                {formatBytes(result.total_db_size_bytes)}
              </span>
              <span className="text-gray-300 dark:text-gray-600">·</span>
            </>
          )}
          <span className="text-gray-400 dark:text-gray-500">Gas limit:</span>
          <span className="text-gray-600 dark:text-gray-300">{compactNumber(flags.gas_limit)}</span>
          {result?.state_root && (
            <>
              <span className="text-gray-300 dark:text-gray-600">·</span>
              <span className="text-gray-400 dark:text-gray-500">State root:</span>
              <span className="font-mono text-gray-600 dark:text-gray-300" title={result.state_root}>
                {shortHash(result.state_root)}
              </span>
            </>
          )}
        </span>
      }
      collapsible
      defaultCollapsed
    >
      <div className="flex flex-col gap-4">
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          <a
            href="https://github.com/ethereum/state-actor"
            target="_blank"
            rel="noreferrer"
            className="font-medium text-blue-600 hover:underline dark:text-blue-400"
          >
            state-actor
          </a>{' '}
          is the tool that builds a client&apos;s data directory — the synthetic
          genesis state the benchmark boots from and replays payloads on. The
          manifest below records how this run&apos;s snapshot was generated.
        </p>

        <Section title="Build">
          <Field
            label="Version"
            value={
              <span className="inline-flex flex-wrap items-center gap-1.5">
                <Badge variant="info">{sa.version || 'unknown'}</Badge>
                {sa.vcs_modified && <Badge variant="warning">modified</Badge>}
              </span>
            }
          />
          <Field label="Go version" value={sa.go_version} mono />
          <Field label="Platform" value={`${sa.os}/${sa.arch}`} />
          {sa.vcs_revision && <Field label="Revision" value={sa.vcs_revision} mono />}
          {sa.vcs_time && <Field label="Built" value={sa.vcs_time} />}
          <Field label="Generated at" value={manifest.generated_at} />
        </Section>

        <Section title="Flags">
          <Field label="Client" value={flags.client} />
          <Field label="Fork" value={flags.fork} />
          <Field label="Seed" value={String(flags.seed)} mono />
          <Field label="Chain ID" value={String(flags.chain_id)} />
          <Field label="Gas limit" value={formatNumber(flags.gas_limit)} />
          {flags.target_size && <Field label="Target size" value={flags.target_size} />}
          <Field label="Group depth" value={String(flags.group_depth)} />
          <Field label="Binary trie" value={flags.binary_trie ? 'yes' : 'no'} />
          <Field label="Archive" value={flags.archive ? 'yes' : 'no'} />
          {flags.spec_path && <Field label="Spec path" value={flags.spec_path} mono />}
          <Field label="DB" value={flags.db} mono />
        </Section>

        {result && (
          <Section title="Result">
            <Field label="State root" value={result.state_root} mono />
            <Field label="Accounts created" value={formatNumber(result.accounts_created)} />
            <Field label="Contracts created" value={formatNumber(result.contracts_created)} />
            <Field label="Storage slots" value={formatNumber(result.storage_slots)} />
            <Field label="DB size" value={formatBytes(result.total_db_size_bytes)} />
            <Field label="Elapsed" value={`${formatNumber(result.elapsed_ms)} ms`} />
          </Section>
        )}

        {spec && (
          <Section title="Spec">
            <Field label="Input path" value={spec.input_path} mono />
            <Field label="SHA256" value={spec.sha256} mono />
            <Field label="Sidecar file" value={spec.output_file} mono />
          </Section>
        )}

        {manifest.reproduced_from && (
          <Section title="Reproduced from">
            <Field label="Manifest" value={manifest.reproduced_from} mono />
          </Section>
        )}

        <div className="flex flex-col gap-1.5 border-t border-gray-200 pt-4 dark:border-gray-700">
          <h4 className="text-xs/5 font-semibold tracking-wide text-gray-500 uppercase dark:text-gray-400">
            Command
          </h4>
          <pre className="overflow-x-auto rounded-sm bg-gray-100 p-2 font-mono text-xs/5 text-gray-900 dark:bg-gray-900 dark:text-gray-100">
            {manifest.command.join(' ')}
          </pre>
        </div>

        <div className="flex flex-col gap-2 border-t border-gray-200 pt-4 dark:border-gray-700">
          <h4 className="text-xs/5 font-semibold tracking-wide text-gray-500 uppercase dark:text-gray-400">
            Raw files
          </h4>
          <RawFile runId={runId} name="state-actor-manifest.json" />
          {spec?.output_file && <RawFile runId={runId} name={spec.output_file} />}
        </div>
      </div>
    </Card>
  )
}
