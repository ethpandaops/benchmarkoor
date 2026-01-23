export const clientColors: Record<string, { bg: string; text: string; darkBg: string; darkText: string }> = {
  geth: {
    bg: 'bg-blue-100',
    text: 'text-blue-800',
    darkBg: 'dark:bg-blue-900/50',
    darkText: 'dark:text-blue-200',
  },
  reth: {
    bg: 'bg-orange-100',
    text: 'text-orange-800',
    darkBg: 'dark:bg-orange-900/50',
    darkText: 'dark:text-orange-200',
  },
  nethermind: {
    bg: 'bg-purple-100',
    text: 'text-purple-800',
    darkBg: 'dark:bg-purple-900/50',
    darkText: 'dark:text-purple-200',
  },
  besu: {
    bg: 'bg-green-100',
    text: 'text-green-800',
    darkBg: 'dark:bg-green-900/50',
    darkText: 'dark:text-green-200',
  },
  erigon: {
    bg: 'bg-red-100',
    text: 'text-red-800',
    darkBg: 'dark:bg-red-900/50',
    darkText: 'dark:text-red-200',
  },
  nimbus: {
    bg: 'bg-yellow-100',
    text: 'text-yellow-800',
    darkBg: 'dark:bg-yellow-900/50',
    darkText: 'dark:text-yellow-200',
  },
}

export function getClientColors(client: string) {
  return (
    clientColors[client] ?? {
      bg: 'bg-gray-100',
      text: 'text-gray-800',
      darkBg: 'dark:bg-gray-700',
      darkText: 'dark:text-gray-200',
    }
  )
}
