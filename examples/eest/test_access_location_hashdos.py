"""Stateful high-gas benchmark for Besu EIP-7928 slot tracking."""

import pytest

from execution_testing import (
    Alloc,
    BenchmarkTestFiller,
    Block,
    Bytecode,
    Fork,
    Hash,
    TestPhaseManager,
    Transaction,
)


# Compiled from SlotHashCollision.sol with solc 0.7.6, optimizer enabled,
# 1,000,000 runs, and bytecodeHash=none. Runtime SHA-256:
# f3c11feb199a29c3b3a04be91051917b0e3b8db4c1fca241f48c7a1554767890
RUNTIME_HEX = (
    "6080604052348015600f57600080fd5b50600080356020803560403581840164010000000081118582101715"
    "6032578586fd5b858652845b81811015605d57604683826096565b8682038602860181905254875118875260"
    "01016037565b5092935083925050810281015b61ffff5a116076576091565b82515484511884529181019180"
    "831415608d578192505b606a565b508083f35b7d01e101e101e101e101e101e101e101e101e101e101e101e1"
    "01e101e101e18160005b61010081101560df576003841660e102811b830192508360021c9350601081019050"
    "60b9565b509092029190911891905056fea164736f6c637828302e372e362d646576656c6f702e323032302e"
    "31312e32332b636f6d6d69742e39316338386135660030"
)
EXECUTION_GAS_CAP = 16_777_216
INDEX_STRIDE = 100_000
CONTROL_SALT = int(
    "9e3779b97f4a7c15f39cc0605cedc835"
    "1082276bf3a27251f86c6a11d0c18e95",
    16,
)


@pytest.mark.parametrize(
    "salt",
    [
        pytest.param(0, id="colliding_hashes"),
        pytest.param(CONTROL_SALT, id="dispersed_hashes_control"),
    ],
)
@pytest.mark.parametrize(
    "keys_per_transaction",
    [
        pytest.param(1_500, id="keys_1500"),
        pytest.param(2_500, id="keys_2500"),
        pytest.param(3_500, id="keys_3500"),
        pytest.param(4_000, id="keys_4000"),
    ],
)
def test_access_location_slot_hashes(
    benchmark_test: BenchmarkTestFiller,
    pre: Alloc,
    fork: Fork,
    gas_benchmark_value: int,
    salt: int,
    keys_per_transaction: int,
) -> None:
    """Fill one block with same-sender calls to the tracker benchmark runtime."""
    del fork  # The workload deliberately pins Amsterdam's execution cap below.

    runtime = Bytecode(
        bytes.fromhex(RUNTIME_HEX),
        popped_stack_items=0,
        pushed_stack_items=0,
    )
    target = pre.deploy_contract(code=runtime)

    # EIP-8037 lets tx.gas exceed EIP-7825 through the state reservoir, but
    # SLOAD consumes execution gas only. Split the requested block gas across
    # capped transactions so 200M exercises roughly 200M of tracker-bearing
    # execution rather than one 16.7M execution budget plus unused state gas.
    gas_left = gas_benchmark_value
    gas_schedule: list[int] = []
    while gas_left > 0:
        tx_gas = min(EXECUTION_GAS_CAP, gas_left)
        # A smaller tail could not cover intrinsic gas plus the distinct-key phase.
        if tx_gas < 10_000_000:
            break
        gas_schedule.append(tx_gas)
        gas_left -= tx_gas

    assert gas_schedule
    assert sum(gas_schedule) <= gas_benchmark_value

    with TestPhaseManager.execution():
        sender = pre.fund_eoa()
        txs = []
        for tx_index, gas_limit in enumerate(gas_schedule):
            start = tx_index * INDEX_STRIDE
            # A 200M schedule has eleven full-cap transactions and a 15.45M
            # tail. Four thousand generated cold keys fit the former, while the
            # tail needs the last known-valid 3,500-key setting.
            tx_key_count = (
                min(keys_per_transaction, 3_500)
                if gas_limit < EXECUTION_GAS_CAP
                else keys_per_transaction
            )
            data = Hash(start) + Hash(tx_key_count) + Hash(salt)
            txs.append(
                Transaction(
                    sender=sender,
                    to=target,
                    gas_limit=gas_limit,
                    data=data,
                )
            )

    benchmark_test(
        pre=pre,
        blocks=[Block(txs=txs)],
        skip_gas_used_validation=True,
        expected_receipt_status=1,
    )
