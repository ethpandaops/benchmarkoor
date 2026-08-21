# EEST 300 Mgas known failures (nethermind glamsterdam-devnet-6)

When filling `tests/benchmark/stateful/bloatnet` with `marker: repricing` at
`gas_benchmark_values: [300]` (300 Mgas) against the patched
`nethermind:glamsterdam-devnet-6-nobuilderdeposit` build, **11 of the 133
selected tests fail**. They are excluded via the `filter:` in
`examples/configuration/config.existing-snapshot-eest.simple.amsterdam.stateful.pre-runs.yaml`.

This list is client- and gas-value-specific. It is **not** a benchmarkoor bug
and **not** a snapshot problem — the pre-run → cross-to-amsterdam → fill
pipeline is correct. The failures are a gas-schedule disagreement between the
`full-benchmark-target` EEST fork model and this nethermind's implementation.

## Root cause

Every failing test is a **value-bearing call to a _contract_ account**
(`CALL` with value, or an ether transfer whose receiver is a contract). EEST
sizes each transaction's `gas_limit` to a predicted `iteration_cost` computed
from its fork gas model. On this nethermind build the **actual** gas charged for
that operation is *higher* than the model predicts, so a transaction handed
exactly `iteration_cost` gas runs out and reverts — `receipt status 0`, which
EEST's `validate_receipt_status` rejects (it exists to catch "silent OOG
failures that roll back state").

This was confirmed by instrumenting the fill to dump `gas_used` vs `gas_limit`
on every failing tx: **`gas_used == gas_limit` exactly** — the signature of
out-of-gas, not an early explicit revert. Examples:

| test | tx gas_limit | gas_used | verdict |
|---|---|---|---|
| `test_account_access` (single tx) | 9,999,037 | 9,999,037 | OOG |
| `test_ether_transfers` (each of ~14k txs) | 21,012 | 21,012 | OOG |

Two of the failing blocks additionally tripped nethermind's own block validation:
`HeaderGasUsedMismatch: Expected 248,975,700, got 816,741,050` — i.e. the actual
gas was ~3.3× the header's expected for that variant.

**Peers that pass confirm the pattern:** value transfers to plain **EOA**
receivers (`diff_to_existent`, `diff_to_nonexistent`, `diff_to_self`), the
`CALLCODE` account_access variants, and the `overhead_baseline_True` variants
all fill cleanly. Only the value-to-contract cases fail.

Note: at 10 Mgas only 3 of these fail; going to 300 Mgas *widens* the mismatch
(3 → 11) because the larger budget packs the workload tighter against the
mispriced per-op cost. Raising the gas value does not fix it.

## The 11 excluded tests

### `test_account_query.py::test_account_access` (4)

All are `opcode_CALL` + `value_sent_1` + `overhead_baseline_False`, across the
four `EXISTING_CONTRACT_*` target modes. Reason: **OOG** — the CALL-with-value
benchmark tx exhausts its ~300 Mgas budget and reverts (`receipt status 0`).

- `...-opcode_CALL-value_sent_1-account_mode_AccountMode.EXISTING_CONTRACT_MINIMAL-overhead_baseline_False-...`
- `...-opcode_CALL-value_sent_1-account_mode_AccountMode.EXISTING_CONTRACT_SAME_MAX-overhead_baseline_False-...`
- `...-opcode_CALL-value_sent_1-account_mode_AccountMode.EXISTING_CONTRACT_DIFF_MAX-overhead_baseline_False-...`
- `...-opcode_CALL-value_sent_1-account_mode_AccountMode.EXISTING_CONTRACT_JUMPDEST-overhead_baseline_False-...`

### `test_transaction_types.py::test_ether_transfers_onchain_receivers` (7)

All are value transfers to **contract** receivers. Each per-tx `gas_limit`
(`iteration_cost`, ~21,012) is less than what nethermind charges, so every tx
OOGs (`receipt status 0`); the block returns ~14k reverted txs.

- `...-transfer_amount_1-case_id_diff_to_contract_minimal-...`
- `...-transfer_amount_1-case_id_diff_to_contract_same_max-...`
- `...-transfer_amount_1-case_id_diff_to_contract_diff_max-...`
- `...-transfer_amount_1-case_id_diff_to_delegated_contract_diff-...`
- `...-transfer_amount_0-case_id_diff_to_delegated_contract_diff-...`
- `...-transfer_amount_1-case_id_diff_to_unique_code_jumpdest_contract-...`
- `...-transfer_amount_0-case_id_diff_to_unique_code_jumpdest_contract-...`

(`diff_to_contract_{minimal,same_max,diff_max}` fail only for `transfer_amount_1`;
`diff_to_delegated_contract_diff` and `diff_to_unique_code_jumpdest_contract`
fail for both amounts.)

## The filter that excludes exactly these 11

```
not ( (test_account_access and opcode_CALL and not CALLCODE and value_sent_1 and overhead_baseline_False and EXISTING_CONTRACT) or (transfer_amount_1 and diff_to_contract_minimal) or (transfer_amount_1 and diff_to_contract_same_max) or (transfer_amount_1 and diff_to_contract_diff_max) or diff_to_delegated_contract_diff or diff_to_unique_code_jumpdest_contract )
```

`opcode_CALL and not CALLCODE` isolates `CALL` from `CALLCODE` (pytest `-k`
matches substrings, and `CALL` is a substring of `CALLCODE`). Verified with
`fill-stateful --collect-only`: **exactly 11 deselected**, every passing sibling
(CALLCODE-contract, CALL-to-EOA, `transfer_amount_0` to `diff_to_contract_*`,
the Bittrex `diff_to_contract`) retained.

## Follow-up

This is worth reporting upstream (EEST `full-benchmark-target` and/or the
nethermind team) as a repricing model-vs-implementation mismatch on amsterdam
for value-bearing calls to contract accounts. The `iteration_cost` calculator
disagrees with nethermind by a fixed per-op amount.
