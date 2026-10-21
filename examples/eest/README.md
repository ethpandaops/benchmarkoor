# Local EEST benchmarks

`test_access_location_hashdos.py` compares deliberately colliding and dispersed
storage-slot hashes while Besu builds an EIP-7928 block access list. Configure it
through `builder.eest_payloads.local_test_files` as shown in
[`docs/configuration.md`](../../docs/configuration.md).

At a 200M block target, the test emits 12 transactions because EIP-7825 caps
Amsterdam execution gas at 16,777,216 per transaction. EIP-8037's state-gas
reservoir does not pay for SLOAD, so a single reservoir transaction would not
increase the number of tracker calls beyond that execution cap.

The colliding keys exploit Tuweni `UInt256`'s signed-byte polynomial hash. Each
two-byte lane contributes zero to the recurrence (`31*a + b == 0`), producing
up to `4^16` distinct keys with one Java hash code. The control applies the same
EVM `MUL` and `XOR` operations with a non-zero salt to disperse the hashes.

`SlotHashCollision.sol` is the source for the embedded runtime. It was compiled
with solc 0.7.6, optimizer enabled for 1,000,000 runs, and metadata bytecode
hashing disabled. The expected runtime SHA-256 is
`f3c11feb199a29c3b3a04be91051917b0e3b8db4c1fca241f48c7a1554767890`.
