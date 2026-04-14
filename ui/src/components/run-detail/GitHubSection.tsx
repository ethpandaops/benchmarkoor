import { Github, ExternalLink } from 'lucide-react'

interface GitHubSectionProps {
  labels?: Record<string, string>
}

/**
 * Renders the GitHub metadata section (repository, workflow, ref, sha, etc.)
 * pulled from `github.*` prefixed metadata labels. Used on both the normal
 * and live run detail pages.
 */
export function GitHubSection({ labels }: GitHubSectionProps) {
  if (!labels) return null

  const gh = Object.entries(labels)
    .filter(([k]) => k.startsWith('github.'))
    .reduce<Record<string, string>>((acc, [k, v]) => {
      acc[k.replace('github.', '')] = v
      return acc
    }, {})

  if (Object.keys(gh).length === 0) return null

  const repoUrl = gh.repository ? `https://github.com/${gh.repository}` : undefined
  const commitUrl = repoUrl && gh.sha ? `${repoUrl}/commit/${gh.sha}` : undefined
  const runUrl = repoUrl && gh.run_id ? `${repoUrl}/actions/runs/${gh.run_id}` : undefined
  const jobUrl = runUrl && gh.job_id ? `${runUrl}#step:0:0` : undefined

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <div className="flex items-center gap-2 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <Github className="size-4 text-gray-500 dark:text-gray-400" />
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">GitHub</h3>
        {runUrl && (
          <a
            href={runUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="ml-auto flex items-center gap-1 text-xs text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
          >
            View workflow run <ExternalLink className="size-3" />
          </a>
        )}
      </div>
      <div className="grid grid-cols-2 gap-x-8 gap-y-2 px-4 py-3 text-sm/6 sm:grid-cols-3 lg:grid-cols-4">
        {gh.repository && (
          <Field label="Repository">
            {repoUrl ? (
              <a
                href={repoUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="font-medium text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                {gh.repository}
              </a>
            ) : (
              <p className="font-medium text-gray-900 dark:text-gray-100">{gh.repository}</p>
            )}
          </Field>
        )}
        {gh.workflow && (
          <Field label="Workflow">
            <p className="font-medium text-gray-900 dark:text-gray-100">{gh.workflow}</p>
          </Field>
        )}
        {gh.ref && (
          <Field label="Ref">
            <p className="font-mono text-xs font-medium text-gray-900 dark:text-gray-100">
              {gh.ref.replace('refs/heads/', '').replace('refs/tags/', '')}
            </p>
          </Field>
        )}
        {gh.event_name && (
          <Field label="Event">
            <p className="font-medium text-gray-900 dark:text-gray-100">{gh.event_name}</p>
          </Field>
        )}
        {gh.sha && (
          <Field label="Commit">
            {commitUrl ? (
              <a
                href={commitUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="font-mono text-xs font-medium text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                {gh.sha.slice(0, 8)}
              </a>
            ) : (
              <p className="font-mono text-xs font-medium text-gray-900 dark:text-gray-100">
                {gh.sha.slice(0, 8)}
              </p>
            )}
          </Field>
        )}
        {gh.actor && (
          <Field label="Actor">
            <a
              href={`https://github.com/${gh.actor}`}
              target="_blank"
              rel="noopener noreferrer"
              className="font-medium text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
            >
              {gh.actor}
            </a>
          </Field>
        )}
        {gh.job && (
          <Field label="Job">
            {jobUrl ? (
              <a
                href={jobUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="font-medium text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                {gh.job}
              </a>
            ) : (
              <p className="font-medium text-gray-900 dark:text-gray-100">{gh.job}</p>
            )}
          </Field>
        )}
        {gh.run_number && (
          <Field label="Run Number">
            <p className="font-medium text-gray-900 dark:text-gray-100">#{gh.run_number}</p>
          </Field>
        )}
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-xs text-gray-500 dark:text-gray-400">{label}</p>
      {children}
    </div>
  )
}
