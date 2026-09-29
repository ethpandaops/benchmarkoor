// SPDX-License-Identifier: Apache-2.0
pragma solidity >=0.7.0 <0.8.0;

/// @notice Benchmark-only runtime for Besu's EIP-7928 AccessLocationTracker.
///
/// Calldata is three words: start index, number of distinct keys, and a salt.
/// A zero salt produces distinct UInt256 values with one Tuweni/Java hashCode.
/// A non-zero salt executes the same EVM operations but disperses those hashes.
contract SlotHashCollision {
    fallback() external {
        assembly {
            function slotFor(index, salt) -> slot {
                let originalIndex := index
                // In every two-byte lane, (a,b) is one of
                // (1,-31), (2,-62), (3,-93), (4,-124). Thus
                // 31*a+b is zero and all 4^16 keys share hashCode().
                slot := 0x01e101e101e101e101e101e101e101e101e101e101e101e101e101e101e1
                for { let shift := 0 } lt(shift, 256) { shift := add(shift, 16) } {
                    slot := add(slot, shl(shift, mul(225, and(index, 3))))
                    index := shr(2, index)
                }

                // Both benchmark arms execute MUL and XOR. salt=0 retains
                // the collision; the control salt disperses the hashes.
                slot := xor(slot, mul(originalIndex, salt))
            }

            let start := calldataload(0)
            let count := calldataload(32)
            let salt := calldataload(64)
            let end := add(start, count)
            if or(lt(end, start), gt(end, 0x100000000)) { revert(0, 0) }
            mstore(0, 0)

            // Populate the tracker's set with distinct keys first.
            for { let index := start } lt(index, end) { index := add(index, 1) } {
                let slot := slotFor(index, salt)
                mstore(add(32, mul(sub(index, start), 32)), slot)
                mstore(0, xor(mload(0), sload(slot)))
            }

            // Cycle over the populated keys. This makes the result robust to
            // the JVM's identity-hash-dependent tree shape: it exercises the
            // average lookup path rather than betting on one key being deep.
            // Reading the keys back from memory also avoids paying to regenerate
            // them on every pass, maximizing tracker lookups per unit of gas.
            let cursor := 32
            let keysEnd := add(32, mul(count, 32))
            for { } gt(gas(), 0xffff) { } {
                mstore(0, xor(mload(0), sload(mload(cursor))))
                cursor := add(cursor, 32)
                if eq(cursor, keysEnd) { cursor := 32 }
            }

            // Make the loads observable so the optimizer cannot delete them.
            return(0, 32)
        }
    }
}
