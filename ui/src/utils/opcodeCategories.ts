export interface OpcodeSubcategory {
  name: string
  opcodes: string[]
}

export interface OpcodeCategory {
  name: string
  opcodes?: string[]
  subcategories?: OpcodeSubcategory[]
}

function pushRange(arr: string[], prefix: string, start: number, end: number): void {
  for (let i = start; i <= end; i++) {
    arr.push(`${prefix}${i}`)
  }
}

function buildPushOpcodes(): string[] {
  const ops = ['PUSH0']
  pushRange(ops, 'PUSH', 1, 32)
  return ops
}

function buildRangeOpcodes(prefix: string, start: number, end: number): string[] {
  const ops: string[] = []
  pushRange(ops, prefix, start, end)
  return ops
}

export const OPCODE_CATEGORIES: OpcodeCategory[] = [
  { name: 'Arithmetic', opcodes: ['ADD', 'MUL', 'SUB', 'DIV', 'SDIV', 'MOD', 'SMOD', 'ADDMOD', 'MULMOD', 'EXP', 'SIGNEXTEND'] },
  { name: 'Comparison', opcodes: ['LT', 'GT', 'SLT', 'SGT', 'EQ', 'ISZERO'] },
  { name: 'Bitwise', opcodes: ['AND', 'OR', 'XOR', 'NOT', 'BYTE', 'SHL', 'SHR', 'SAR'] },
  { name: 'Crypto', opcodes: ['KECCAK256', 'SHA3'] },
  { name: 'Environment', opcodes: ['ADDRESS', 'BALANCE', 'ORIGIN', 'CALLER', 'CALLVALUE', 'CALLDATALOAD', 'CALLDATASIZE', 'CALLDATACOPY', 'CODESIZE', 'CODECOPY', 'GASPRICE', 'EXTCODESIZE', 'EXTCODECOPY', 'RETURNDATASIZE', 'RETURNDATACOPY', 'EXTCODEHASH'] },
  { name: 'Block', opcodes: ['BLOCKHASH', 'COINBASE', 'TIMESTAMP', 'NUMBER', 'PREVRANDAO', 'DIFFICULTY', 'GASLIMIT', 'CHAINID', 'SELFBALANCE', 'BASEFEE', 'BLOBHASH', 'BLOBBASEFEE'] },
  {
    name: 'Stack',
    subcategories: [
      { name: 'Pop', opcodes: ['POP'] },
      { name: 'Push', opcodes: buildPushOpcodes() },
      { name: 'Dup', opcodes: buildRangeOpcodes('DUP', 1, 16) },
      { name: 'Swap', opcodes: buildRangeOpcodes('SWAP', 1, 16) },
    ],
  },
  { name: 'Memory', opcodes: ['MLOAD', 'MSTORE', 'MSTORE8', 'MSIZE', 'MCOPY'] },
  { name: 'Storage', opcodes: ['SLOAD', 'SSTORE'] },
  { name: 'Transient', opcodes: ['TLOAD', 'TSTORE'] },
  { name: 'Control', opcodes: ['STOP', 'JUMP', 'JUMPI', 'PC', 'GAS', 'JUMPDEST'] },
  { name: 'Logging', opcodes: ['LOG0', 'LOG1', 'LOG2', 'LOG3', 'LOG4'] },
  { name: 'System', opcodes: ['CREATE', 'CALL', 'CALLCODE', 'RETURN', 'DELEGATECALL', 'CREATE2', 'STATICCALL', 'REVERT', 'INVALID', 'SELFDESTRUCT'] },
]

/** Get all opcodes for a category (flat, including subcategories) */
function getAllCategoryOpcodes(cat: OpcodeCategory): string[] {
  if (cat.opcodes) return cat.opcodes
  if (cat.subcategories) return cat.subcategories.flatMap((s) => s.opcodes)
  return []
}

const opcodeToCategoryMap = new Map<string, string>()
for (const cat of OPCODE_CATEGORIES) {
  for (const op of getAllCategoryOpcodes(cat)) {
    opcodeToCategoryMap.set(op, cat.name)
  }
}

export function getOpcodeCategory(opcode: string): string {
  return opcodeToCategoryMap.get(opcode) ?? 'Other'
}

/** Color for each category: [light, dark] */
export const CATEGORY_COLORS: Record<string, { light: string; dark: string }> = {
  Arithmetic:  { light: '#dc2626', dark: '#f87171' },
  Comparison:  { light: '#ea580c', dark: '#fb923c' },
  Bitwise:     { light: '#d97706', dark: '#fbbf24' },
  Crypto:      { light: '#65a30d', dark: '#a3e635' },
  Environment: { light: '#059669', dark: '#34d399' },
  Block:       { light: '#0891b2', dark: '#22d3ee' },
  Stack:       { light: '#2563eb', dark: '#60a5fa' },
  Memory:      { light: '#7c3aed', dark: '#a78bfa' },
  Storage:     { light: '#9333ea', dark: '#c084fc' },
  Transient:   { light: '#c026d3', dark: '#e879f9' },
  Control:     { light: '#db2777', dark: '#f472b6' },
  Logging:     { light: '#e11d48', dark: '#fb7185' },
  System:      { light: '#4b5563', dark: '#9ca3af' },
  Other:       { light: '#6b7280', dark: '#9ca3af' },
}

export function getCategoryColor(name: string, isDark: boolean): string {
  const entry = CATEGORY_COLORS[name]
  if (!entry) return isDark ? '#9ca3af' : '#6b7280'
  return isDark ? entry.dark : entry.light
}

/** A subcategory span within a category */
export interface SubcategorySpan {
  name: string
  opcodes: string[]
  startCol: number
  count: number
}

/** A category span, optionally containing subcategory spans */
export interface CategorySpan {
  name: string
  opcodes: string[]
  startCol: number
  count: number
  subcategories?: SubcategorySpan[]
}

export interface GroupedResult {
  /** All opcodes in display order */
  columns: string[]
  /** Category-level spans */
  categorySpans: CategorySpan[]
}

export function getGroupedOpcodes(presentOpcodes: Set<string>): GroupedResult {
  const columns: string[] = []
  const categorySpans: CategorySpan[] = []
  const categorized = new Set<string>()

  for (const cat of OPCODE_CATEGORIES) {
    if (cat.subcategories) {
      const subSpans: SubcategorySpan[] = []
      const catStart = columns.length
      for (const sub of cat.subcategories) {
        const present = sub.opcodes.filter((op) => presentOpcodes.has(op))
        if (present.length > 0) {
          subSpans.push({ name: sub.name, opcodes: present, startCol: columns.length, count: present.length })
          columns.push(...present)
          for (const op of present) categorized.add(op)
        }
      }
      if (subSpans.length > 0) {
        const allOps = subSpans.flatMap((s) => s.opcodes)
        categorySpans.push({ name: cat.name, opcodes: allOps, startCol: catStart, count: allOps.length, subcategories: subSpans })
      }
    } else if (cat.opcodes) {
      const present = cat.opcodes.filter((op) => presentOpcodes.has(op))
      if (present.length > 0) {
        categorySpans.push({ name: cat.name, opcodes: present, startCol: columns.length, count: present.length })
        columns.push(...present)
        for (const op of present) categorized.add(op)
      }
    }
  }

  const other = Array.from(presentOpcodes)
    .filter((op) => !categorized.has(op))
    .sort()
  if (other.length > 0) {
    categorySpans.push({ name: 'Other', opcodes: other, startCol: columns.length, count: other.length })
    columns.push(...other)
  }

  return { columns, categorySpans }
}
