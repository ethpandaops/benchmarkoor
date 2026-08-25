"""Tempo benchmark override for warming KZG point-evaluation setup."""

import pytest
from execution_testing import (
    Address,
    Alloc,
    BenchmarkTestFiller,
    Block,
    Fork,
    JumpLoopGenerator,
    Op,
    TestPhaseManager,
    Transaction,
)

from tests.benchmark.helper.precompile import Precompile
from tests.cancun.eip4844_blobs.spec import PointEvaluationInput
from tests.cancun.eip4844_blobs.spec import Spec as BlobsSpec


POINT_EVALUATION_CALLDATA = PointEvaluationInput(
    versioned_hash=bytes.fromhex(
        "01E798154708FE7789429634053CBF9F"
        "99B619F9F084048927333FCE637F549B"
    ),
    z=0x564C0A11A0F704F4FC3E8ACFE0F8245F0AD1347B378FBF96E206DA11A5D36306,
    y=0x24D25032E67A7E6A4910DF5834B8FE70E6BCFEEAC0352434196BDF4B2485D5A1,
    commitment=bytes.fromhex(
        "8F59A8D2A1A625A17F3FEA0FE5EB8C896DB3764F3185481BC22F91B4AAFFCCA2"
        "5F26936857BC3A7C2539EA8EC3A952B7"
    ),
    proof=bytes.fromhex(
        "873033E038326E87ED3E1276FD140253FA08E9FC25FB2D9A98527FC22A2C9612"
        "FBEAFDAD446CBC7BCDBDCD780AF2C16A"
    ),
)


@pytest.mark.repricing
@pytest.mark.parametrize(
    "precompile_address,calldata,tx_gas_limits",
    [
        pytest.param(
            BlobsSpec.POINT_EVALUATION_PRECOMPILE_ADDRESS,
            POINT_EVALUATION_CALLDATA,
            [1_000_000],
            id="point_evaluation_warm_1m",
        ),
        pytest.param(
            BlobsSpec.POINT_EVALUATION_PRECOMPILE_ADDRESS,
            POINT_EVALUATION_CALLDATA,
            [5_000_000],
            id="point_evaluation_warm_5m",
        ),
        pytest.param(
            BlobsSpec.POINT_EVALUATION_PRECOMPILE_ADDRESS,
            POINT_EVALUATION_CALLDATA,
            [10_000_000],
            id="point_evaluation_warm_10m",
        ),
        pytest.param(
            BlobsSpec.POINT_EVALUATION_PRECOMPILE_ADDRESS,
            POINT_EVALUATION_CALLDATA,
            [15_000_000],
            id="point_evaluation_warm_15m",
        ),
        pytest.param(
            BlobsSpec.POINT_EVALUATION_PRECOMPILE_ADDRESS,
            POINT_EVALUATION_CALLDATA,
            [15_000_000, 15_000_000],
            id="point_evaluation_warm_30m",
        ),
    ],
)
def test_point_evaluation_warm(
    benchmark_test: BenchmarkTestFiller,
    pre: Alloc,
    fork: Fork,
    gas_benchmark_value: int,
    precompile_address: Address,
    calldata: bytes,
    tx_gas_limits: list[int],
) -> None:
    """Benchmark POINT EVALUATION after one setup precompile call."""
    del gas_benchmark_value

    if precompile_address not in fork.precompiles():
        pytest.skip("Precompile not enabled")

    attack_block = Op.POP(
        Op.STATICCALL(
            gas=Op.GAS,
            address=precompile_address,
            args_size=Op.CALLDATASIZE,
        ),
    )
    code_generator = JumpLoopGenerator(
        setup=Op.CALLDATACOPY(0, 0, Op.CALLDATASIZE),
        attack_block=attack_block,
        tx_kwargs={"data": calldata},
    )
    contract_address = code_generator.deploy_contracts(
        pre=pre,
        fork=fork.fork_at(block_number=0, timestamp=0),
    )

    blocks: list[Block] = []
    with TestPhaseManager.setup():
        blocks.append(
            Block(
                txs=[
                    Transaction(
                        to=precompile_address,
                        sender=pre.fund_eoa(),
                        gas_limit=300_000,
                        data=calldata,
                    )
                ]
            )
        )

    with TestPhaseManager.execution():
        blocks.append(
            Block(
                txs=[
                    code_generator.generate_transaction(
                        pre=pre,
                        gas_benchmark_value=tx_gas_limit,
                    )
                    for tx_gas_limit in tx_gas_limits
                ]
            )
        )

    benchmark_test(
        target_opcode=Precompile.POINT_EVALUATION,
        blocks=blocks,
        expected_benchmark_gas_used=sum(tx_gas_limits),
    )
