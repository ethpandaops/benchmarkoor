// Shared types + runtime helpers for the payload-sizes table sort.
// Kept out of TestFilesList.tsx so React Fast Refresh isn't broken by
// non-component exports living alongside the table component.

export type PayloadSortCol =
  | 'index'
  | 'ssz_full'
  | 'ssz_bal'
  | 'ssz_bal_pct'
  | 'ssz_snappy'
  | 'ssz_snappy_pct'
  | 'json_full'
  | 'json_bal'
  | 'json_bal_pct'

export interface PayloadSort {
  col: PayloadSortCol
  dir: 'asc' | 'desc'
}

const PAYLOAD_SORT_COLS: readonly PayloadSortCol[] = [
  'index',
  'ssz_full',
  'ssz_bal',
  'ssz_bal_pct',
  'ssz_snappy',
  'ssz_snappy_pct',
  'json_full',
  'json_bal',
  'json_bal_pct',
]

export function isPayloadSortCol(s: string): s is PayloadSortCol {
  return (PAYLOAD_SORT_COLS as readonly string[]).includes(s)
}
