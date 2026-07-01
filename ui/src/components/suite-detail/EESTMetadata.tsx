import { useMemo } from 'react'

import { useQuery } from '@tanstack/react-query'
import { ExternalLink } from 'lucide-react'

import { fetchText } from '@/api/client'
import { Card } from '@/components/shared/Card'
import { getDataUrl, loadRuntimeConfig } from '@/config/runtime'

interface EESTMetadataProps {
  suiteHash: string
}

interface IniSection {
  name: string
  entries: [string, string][]
}

// parseIni parses a minimal INI document (sections + key=value pairs) into
// ordered sections, preserving order and skipping comments and blank lines.
function parseIni(text: string): IniSection[] {
  const sections: IniSection[] = []
  let current: IniSection | null = null

  for (const rawLine of text.split('\n')) {
    const line = rawLine.trim()
    if (line === '' || line.startsWith(';') || line.startsWith('#')) continue

    const section = line.match(/^\[(.+)\]$/)
    if (section) {
      current = { name: section[1], entries: [] }
      sections.push(current)

      continue
    }

    const eq = line.indexOf('=')
    if (eq === -1) continue

    if (!current) {
      current = { name: '', entries: [] }
      sections.push(current)
    }

    current.entries.push([line.slice(0, eq).trim(), line.slice(eq + 1).trim()])
  }

  return sections
}

// EESTMetadata renders the EEST fill provenance copied into a suite's
// .eest-meta directory: the parsed fixtures.ini (tool/python versions, fill
// command, packages/plugins) plus links to and an embed of the fill report.
// Rendered as a single collapsible card so it sits inside the Source tab.
export function EESTMetadata({ suiteHash }: EESTMetadataProps) {
  const iniQuery = useQuery({
    queryKey: ['eest-meta-fixtures-ini', suiteHash],
    queryFn: () => fetchText(`suites/${suiteHash}/.eest-meta/fixtures.ini`),
  })

  const { data: config } = useQuery({
    queryKey: ['runtime-config'],
    queryFn: loadRuntimeConfig,
    staleTime: Infinity,
  })

  const sections = useMemo(
    () => (iniQuery.data?.data ? parseIni(iniQuery.data.data) : []),
    [iniQuery.data],
  )

  // Don't render the section at all while loading or if the metadata is
  // unreadable — the caller only mounts this when the suite advertises it.
  if (iniQuery.isLoading || iniQuery.data?.data == null) {
    return null
  }

  const reportUrl = config
    ? getDataUrl(`suites/${suiteHash}/.eest-meta/report_fill.html`, config)
    : undefined
  const indexUrl = config
    ? getDataUrl(`suites/${suiteHash}/.eest-meta/index.json`, config)
    : undefined

  return (
    <Card title="EEST Build Metadata" collapsible>
      <div className="flex flex-col gap-4">
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          Provenance recorded by execution-spec-tests when these fixtures were filled
          (<code className="font-mono">.eest-meta/fixtures.ini</code>).
        </p>

        {sections.map((section) => (
          <div
            key={section.name}
            className="flex flex-col gap-2 border-t border-gray-200 pt-4 first:border-t-0 first:pt-0 dark:border-gray-700"
          >
            <h4 className="text-xs/5 font-semibold tracking-wide text-gray-500 uppercase dark:text-gray-400">
              {section.name || 'general'}
            </h4>
            <dl className="flex flex-col gap-1.5">
              {section.entries.map(([key, value]) => (
                <div key={key} className="flex flex-col gap-0.5 sm:flex-row sm:gap-4">
                  <dt className="w-full text-xs/5 font-medium text-gray-600 sm:w-56 sm:shrink-0 dark:text-gray-300">
                    {key}
                  </dt>
                  <dd className="min-w-0 break-all font-mono text-xs/5 text-gray-800 dark:text-gray-200">
                    {value}
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        ))}

        {(reportUrl || indexUrl) && (
          <div className="flex flex-col gap-3 border-t border-gray-200 pt-4 dark:border-gray-700">
            <h4 className="text-xs/5 font-semibold tracking-wide text-gray-500 uppercase dark:text-gray-400">
              fill report
            </h4>
            <div className="flex flex-wrap gap-2">
              {reportUrl && (
                <a
                  href={reportUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1.5 rounded-xs bg-blue-600 px-3 py-1.5 text-xs/5 font-medium text-white hover:bg-blue-700"
                >
                  Open fill report
                  <ExternalLink className="size-3.5" />
                </a>
              )}
              {indexUrl && (
                <a
                  href={indexUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1.5 rounded-xs border border-gray-300 px-3 py-1.5 text-xs/5 font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
                >
                  View fixture index (JSON)
                  <ExternalLink className="size-3.5" />
                </a>
              )}
            </div>
            {reportUrl && (
              <div className="overflow-hidden rounded-xs border border-gray-200 dark:border-gray-700">
                {/* The fill report is a light-mode HTML page rendered in an iframe
                    (often cross-origin, so its document can't be styled from here).
                    Approximate dark mode by inverting the rendered frame when the
                    UI is in dark mode. invert(.9) (not full 1.0) softens the
                    extremes — white bg → dark gray (~#1a1a1a) and black text →
                    near-white — so it blends with the UI palette instead of going
                    stark black/white; hue-rotate keeps hues roughly correct. Tracks
                    the UI theme automatically via the dark variant. */}
                <iframe
                  title="EEST fill report"
                  src={reportUrl}
                  className="h-[60vh] w-full bg-white dark:invert-[.9] dark:hue-rotate-180"
                />
              </div>
            )}
          </div>
        )}
      </div>
    </Card>
  )
}
