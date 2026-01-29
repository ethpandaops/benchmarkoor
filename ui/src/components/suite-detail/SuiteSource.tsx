import type { SourceInfo } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Card } from '@/components/shared/Card'

interface SuiteSourceProps {
  title: string
  source: SourceInfo
}

interface StepsGlobs {
  setup?: string[]
  test?: string[]
  cleanup?: string[]
}

function StepsInfo({ steps, preRunSteps }: { steps?: StepsGlobs; preRunSteps?: string[] }) {
  const hasSteps = steps && (steps.setup?.length || steps.test?.length || steps.cleanup?.length)
  const hasPreRunSteps = preRunSteps && preRunSteps.length > 0

  if (!hasSteps && !hasPreRunSteps) return null

  return (
    <div className="flex flex-col gap-3 border-t border-gray-200 pt-4 dark:border-gray-700">
      <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Test Discovery Patterns</dt>
      <dd className="flex flex-col gap-2">
        {hasPreRunSteps && (
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-gray-600 dark:text-gray-300">Pre-Run Steps</span>
            <div className="flex flex-wrap gap-1">
              {preRunSteps!.map((pattern, i) => (
                <code
                  key={i}
                  className="rounded-xs bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-700 dark:bg-gray-700 dark:text-gray-300"
                >
                  {pattern}
                </code>
              ))}
            </div>
          </div>
        )}
        {steps?.setup && steps.setup.length > 0 && (
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-gray-600 dark:text-gray-300">Setup</span>
            <div className="flex flex-wrap gap-1">
              {steps.setup.map((pattern, i) => (
                <code
                  key={i}
                  className="rounded-xs bg-blue-50 px-1.5 py-0.5 font-mono text-xs text-blue-700 dark:bg-blue-900/30 dark:text-blue-300"
                >
                  {pattern}
                </code>
              ))}
            </div>
          </div>
        )}
        {steps?.test && steps.test.length > 0 && (
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-gray-600 dark:text-gray-300">Test</span>
            <div className="flex flex-wrap gap-1">
              {steps.test.map((pattern, i) => (
                <code
                  key={i}
                  className="rounded-xs bg-green-50 px-1.5 py-0.5 font-mono text-xs text-green-700 dark:bg-green-900/30 dark:text-green-300"
                >
                  {pattern}
                </code>
              ))}
            </div>
          </div>
        )}
        {steps?.cleanup && steps.cleanup.length > 0 && (
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-gray-600 dark:text-gray-300">Cleanup</span>
            <div className="flex flex-wrap gap-1">
              {steps.cleanup.map((pattern, i) => (
                <code
                  key={i}
                  className="rounded-xs bg-orange-50 px-1.5 py-0.5 font-mono text-xs text-orange-700 dark:bg-orange-900/30 dark:text-orange-300"
                >
                  {pattern}
                </code>
              ))}
            </div>
          </div>
        )}
      </dd>
    </div>
  )
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
    </svg>
  )
}

function getGitHubUrl(repo: string, sha?: string, directory?: string): string {
  let baseUrl = repo
  if (repo.startsWith('git@github.com:')) {
    baseUrl = repo.replace('git@github.com:', 'https://github.com/').replace(/\.git$/, '')
  } else if (repo.includes('github.com') && repo.endsWith('.git')) {
    baseUrl = repo.replace(/\.git$/, '')
  } else if (!repo.startsWith('http')) {
    baseUrl = `https://github.com/${repo}`
  }

  if (sha && directory) {
    return `${baseUrl}/tree/${sha}/${directory}`
  } else if (sha) {
    return `${baseUrl}/tree/${sha}`
  }
  return baseUrl
}

function SourceTypeBadge({ source }: { source: SourceInfo }) {
  if (source.git) {
    return <Badge variant="info">Git</Badge>
  }

  if (source.local) {
    return <Badge variant="warning">Local</Badge>
  }

  if (source.eest) {
    const hasArtifacts =
      source.eest.fixtures_artifact_name || source.eest.genesis_artifact_name
    return <Badge variant="success">{hasArtifacts ? 'EEST Artifact' : 'EEST Release'}</Badge>
  }

  return null
}

export function SuiteSource({ title, source }: SuiteSourceProps) {
  if (source.git) {
    const gitUrl = getGitHubUrl(source.git.repo, source.git.sha)

    return (
      <Card title={<span className="flex items-center gap-2">{title}<SourceTypeBadge source={source} /></span>} collapsible>
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Repository</dt>
              <dd className="mt-1 break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.git.repo}</dd>
            </div>
            <div>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Version</dt>
              <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.git.version}</dd>
            </div>
            <div>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Commit SHA</dt>
              <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.git.sha}</dd>
            </div>
          </dl>
          <StepsInfo steps={source.git.steps} preRunSteps={source.git.pre_run_steps} />
          <a
            href={gitUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex w-fit items-center gap-2 rounded-sm bg-gray-900 px-3 py-1.5 text-sm/6 font-medium text-white hover:bg-gray-700 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-300"
          >
            <GitHubIcon className="size-4" />
            View on GitHub
          </a>
        </div>
      </Card>
    )
  }

  if (source.local) {
    return (
      <Card title={<span className="flex items-center gap-2">{title}<SourceTypeBadge source={source} /></span>} collapsible>
        <div className="flex flex-col gap-4">
          <div>
            <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Local Directory</dt>
            <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.local.base_dir}</dd>
          </div>
          <StepsInfo steps={source.local.steps} preRunSteps={source.local.pre_run_steps} />
        </div>
      </Card>
    )
  }

  if (source.eest) {
    const eest = source.eest
    const repoUrl = getGitHubUrl(eest.github_repo)
    const runId = eest.fixtures_artifact_run_id || eest.genesis_artifact_run_id
    const githubLink = runId
      ? `${repoUrl}/actions/runs/${runId}`
      : eest.github_release
        ? `${repoUrl}/releases/tag/${eest.github_release}`
        : repoUrl
    const githubLinkLabel = runId
      ? 'View Actions Run'
      : eest.github_release
        ? 'View Release'
        : 'View on GitHub'
    const hasArtifacts =
      eest.fixtures_artifact_name || eest.genesis_artifact_name || eest.fixtures_artifact_run_id || eest.genesis_artifact_run_id

    return (
      <Card title={<span className="flex items-center gap-2">{title}<SourceTypeBadge source={source} /></span>} collapsible>
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Repository</dt>
              <dd className="mt-1 break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">{eest.github_repo}</dd>
            </div>
            {eest.github_release && (
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Release</dt>
                <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{eest.github_release}</dd>
              </div>
            )}
            {eest.fixtures_subdir && (
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Fixtures Subdirectory</dt>
                <dd className="mt-1">
                  <code className="rounded-xs bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-700 dark:bg-gray-700 dark:text-gray-300">
                    {eest.fixtures_subdir}
                  </code>
                </dd>
              </div>
            )}
          </dl>
          {(eest.fixtures_url || eest.genesis_url) && (
            <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              {eest.fixtures_url && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Fixtures URL</dt>
                  <dd className="mt-1 break-all text-sm/6">
                    <a
                      href={eest.fixtures_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-blue-600 hover:underline dark:text-blue-400"
                    >
                      {eest.fixtures_url}
                    </a>
                  </dd>
                </div>
              )}
              {eest.genesis_url && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis URL</dt>
                  <dd className="mt-1 break-all text-sm/6">
                    <a
                      href={eest.genesis_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-blue-600 hover:underline dark:text-blue-400"
                    >
                      {eest.genesis_url}
                    </a>
                  </dd>
                </div>
              )}
            </dl>
          )}
          {hasArtifacts && (
            <div className="flex flex-col gap-3 border-t border-gray-200 pt-4 dark:border-gray-700">
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Artifacts</dt>
              <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                {eest.fixtures_artifact_name && (
                  <div>
                    <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Fixtures Artifact</dt>
                    <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{eest.fixtures_artifact_name}</dd>
                  </div>
                )}
                {eest.genesis_artifact_name && (
                  <div>
                    <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis Artifact</dt>
                    <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{eest.genesis_artifact_name}</dd>
                  </div>
                )}
                {eest.fixtures_artifact_run_id && (
                  <div>
                    <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Fixtures Run ID</dt>
                    <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{eest.fixtures_artifact_run_id}</dd>
                  </div>
                )}
                {eest.genesis_artifact_run_id && (
                  <div>
                    <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis Run ID</dt>
                    <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{eest.genesis_artifact_run_id}</dd>
                  </div>
                )}
              </dl>
            </div>
          )}
          <a
            href={githubLink}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex w-fit items-center gap-2 rounded-xs bg-gray-900 px-3 py-1.5 text-sm/6 font-medium text-white hover:bg-gray-700 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-300"
          >
            <GitHubIcon className="size-4" />
            {githubLinkLabel}
          </a>
        </div>
      </Card>
    )
  }

  return null
}
