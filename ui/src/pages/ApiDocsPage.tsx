import { useEffect, useState } from 'react'
import { ApiReferenceReact } from '@scalar/api-reference-react'
import '@scalar/api-reference-react/style.css'
import { loadRuntimeConfig } from '@/config/runtime'

export function ApiDocsPage() {
  const [specUrl, setSpecUrl] = useState<string | null>(null)
  const [isDark, setIsDark] = useState(() =>
    document.documentElement.classList.contains('dark'),
  )

  useEffect(() => {
    loadRuntimeConfig().then((cfg) => {
      if (cfg.api?.baseUrl) {
        setSpecUrl(`${cfg.api.baseUrl}/api/v1/openapi.json`)
      }
    })
  }, [])

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
    return () => observer.disconnect()
  }, [])

  if (!specUrl) {
    return (
      <div className="flex min-h-64 items-center justify-center text-gray-500 dark:text-gray-400">
        API not configured
      </div>
    )
  }

  return (
    <div className="-mx-4 -mb-8 -mt-8 min-h-0 flex-1">
      <ApiReferenceReact
        configuration={{
          url: specUrl,
          darkMode: isDark,
          hideDarkModeToggle: true,
          hideDownloadButton: true,
        }}
      />
    </div>
  )
}
