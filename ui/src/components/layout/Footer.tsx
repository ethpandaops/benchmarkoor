import packageJson from '../../../package.json'

export function Footer() {
  return (
    <footer className="border-t border-gray-200 bg-white py-6 dark:border-gray-800 dark:bg-gray-900">
      <div className="mx-auto max-w-7xl px-4 text-center text-sm text-gray-500 dark:text-gray-400">
        <span>Powered by 🐼 </span>
        <a
          href="https://github.com/ethpandaops/benchmarkoor"
          target="_blank"
          rel="noopener noreferrer"
          className="font-bold text-gray-700 hover:text-gray-900 dark:text-gray-300 dark:hover:text-gray-100"
        >
          ethpandaops/benchmarkoor
        </a>
        <span className="mx-2">•</span>
        <span>v{packageJson.version}</span>
      </div>
    </footer>
  )
}
