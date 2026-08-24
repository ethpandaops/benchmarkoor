#!/usr/bin/env python3
"""Export canonical Tempo blocks as a Benchmarkoor semantic Engine suite."""

from __future__ import annotations

import argparse
import json
import shutil
from pathlib import Path
from typing import Any
from urllib import request


FORMAT = "tempo-engine-suite/v1"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Export canonical blocks from a running Tempo node for Benchmarkoor"
    )
    parser.add_argument("--rpc-url", default="http://127.0.0.1:8545")
    parser.add_argument("--genesis", required=True, type=Path)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--name", required=True)
    parser.add_argument("--from-block", type=int, default=1)
    parser.add_argument("--to-block", type=int)
    parser.add_argument("--capture-file", type=Path)
    parser.add_argument("--description", default="Tempo block execution replay")
    parser.add_argument("--tag", action="append", default=[])
    parser.add_argument("--seed", default="0")
    parser.add_argument("--origin-kind", default="tempo-native")
    parser.add_argument(
        "--origin-repository", default="https://github.com/tempoxyz/tempo"
    )
    parser.add_argument(
        "--generator", default="benchmarkoor integrations/tempo/export-suite.py"
    )
    parser.add_argument("--metadata", action="append", default=[])
    parser.add_argument("--revision", default="unknown")
    parser.add_argument("--hardfork", default="unknown")
    parser.add_argument("--chain-id", type=int, default=0)
    parser.add_argument(
        "--block-access-lists",
        action=argparse.BooleanOptionalAction,
        default=True,
    )
    parser.add_argument(
        "--wait-for-caches",
        action=argparse.BooleanOptionalAction,
        default=True,
    )
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()

    if not args.genesis.is_file():
        parser.error("--genesis must name an existing file")
    if args.capture_file:
        if not args.capture_file.is_file():
            parser.error("--capture-file must name an existing file")
        if args.to_block is not None:
            parser.error("--capture-file conflicts with --to-block")
    elif args.to_block is None:
        parser.error("--to-block is required unless --capture-file is supplied")
    elif args.from_block < 1 or args.to_block < args.from_block:
        parser.error("the block range must satisfy 1 <= from-block <= to-block")
    return args


class RpcClient:
    def __init__(self, url: str) -> None:
        self.url = url
        self.request_id = 0

    def call(self, method: str, params: list[Any]) -> Any:
        self.request_id += 1
        payload = json.dumps(
            {
                "jsonrpc": "2.0",
                "id": self.request_id,
                "method": method,
                "params": params,
            }
        ).encode()
        rpc_request = request.Request(
            self.url, data=payload, headers={"content-type": "application/json"}
        )
        with request.urlopen(rpc_request, timeout=60) as response:
            body = json.load(response)
        if body.get("error") is not None:
            raise RuntimeError(f"{method} failed: {body['error']}")
        if "result" not in body:
            raise RuntimeError(f"{method} returned no result")
        return body["result"]


def quantity(value: str) -> int:
    return int(value, 16)


def block_transaction_count(rpc: RpcClient, number: int) -> int:
    block = rpc.call("eth_getBlockByNumber", [hex(number), False])
    if block is None:
        raise RuntimeError(f"source node has no block {number}")
    return len(block.get("transactions", []))


def export_block(
    rpc: RpcClient,
    blocks_dir: Path,
    number: int,
    include_bal: bool,
    wait_for_caches: bool,
) -> list[dict[str, Any]]:
    block_id = hex(number)
    block = rpc.call("eth_getBlockByNumber", [block_id, False])
    if block is None:
        raise RuntimeError(f"source node has no block {number}")

    block_hash = block["hash"]
    rlp_name = f"{number}.rlp"
    (blocks_dir / rlp_name).write_text(
        rpc.call("debug_getRawBlock", [block_id]), encoding="utf-8"
    )

    payload_call: dict[str, Any] = {
        "rlp_file": f"blocks/{rlp_name}",
        "wait_for_caches": wait_for_caches,
        "block_number": number,
        "block_hash": block_hash,
        "gas_used": quantity(block["gasUsed"]),
        "transaction_count": len(block.get("transactions", [])),
        "expected_status": "VALID",
    }
    if include_bal:
        bal_name = f"{number}.bal"
        (blocks_dir / bal_name).write_text(
            rpc.call("debug_getRawBlockAccessList", [block_id]), encoding="utf-8"
        )
        payload_call["bal_file"] = f"blocks/{bal_name}"

    forkchoice_call = {
        "method": "reth_forkchoiceUpdated",
        "params": [
            {
                "headBlockHash": block_hash,
                "safeBlockHash": block_hash,
                "finalizedBlockHash": block_hash,
            }
        ],
        "block_number": number,
        "block_hash": block_hash,
        "expected_status": "VALID",
    }
    return [payload_call, forkchoice_call]


def load_capture(path: Path) -> list[dict[str, Any]]:
    records = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        try:
            records.append(json.loads(line))
        except json.JSONDecodeError as error:
            raise ValueError(f"invalid capture JSON at {path}:{line_number}") from error
    if not records:
        raise ValueError("capture file contains no records")
    return records


def export_capture(
    args: argparse.Namespace, rpc: RpcClient, blocks_dir: Path
) -> tuple[list[dict[str, Any]], dict[str, str]]:
    records = load_capture(args.capture_file)
    tests = []
    next_block = 1
    passed_without_transaction = 0

    for record in records:
        if record["after_block"] < record["before_block"]:
            raise ValueError(f"capture interval moved backwards for {record['nodeid']}")
        if record["outcome"] != "passed":
            continue

        measured_block = None
        for number in range(record["before_block"] + 1, record["after_block"] + 1):
            if block_transaction_count(rpc, number) > 0:
                measured_block = number
        if measured_block is None:
            passed_without_transaction += 1
            continue
        if measured_block < next_block:
            raise ValueError(
                f"captured block {measured_block} for {record['nodeid']} was already exported"
            )

        setup: list[dict[str, Any]] = []
        test: list[dict[str, Any]] = []
        for number in range(next_block, measured_block + 1):
            calls = export_block(
                rpc,
                blocks_dir,
                number,
                args.block_access_lists,
                args.wait_for_caches,
            )
            (test if number == measured_block else setup).extend(calls)
        next_block = measured_block + 1
        tests.append(
            {
                "name": record["nodeid"],
                "description": args.description,
                "tags": args.tag,
                "metadata": {
                    "capture_before_block": str(record["before_block"]),
                    "capture_after_block": str(record["after_block"]),
                    "measured_block": str(measured_block),
                },
                "setup": setup,
                "test": test,
            }
        )

    if not tests:
        raise ValueError("capture contains no passing transaction-bearing tests")
    passed = sum(record["outcome"] == "passed" for record in records)
    return tests, {
        "capture_record_count": str(len(records)),
        "capture_passed_count": str(passed),
        "capture_failed_count": str(len(records) - passed),
        "capture_exported_count": str(len(tests)),
        "capture_passed_without_transaction_count": str(passed_without_transaction),
    }


def export_range(
    args: argparse.Namespace, rpc: RpcClient, blocks_dir: Path
) -> list[dict[str, Any]]:
    setup: list[dict[str, Any]] = []
    test: list[dict[str, Any]] = []
    for number in range(1, args.to_block + 1):
        calls = export_block(
            rpc,
            blocks_dir,
            number,
            args.block_access_lists,
            args.wait_for_caches,
        )
        (setup if number < args.from_block else test).extend(calls)
    return [
        {
            "name": args.name,
            "description": args.description,
            "tags": args.tag,
            "metadata": {},
            "setup": setup,
            "test": test,
        }
    ]


def main() -> None:
    args = parse_args()
    manifest_path = args.out / "manifest.json"
    if manifest_path.exists() and not args.force:
        raise FileExistsError(f"{manifest_path} exists; pass --force to replace it")

    blocks_dir = args.out / "blocks"
    if args.force and blocks_dir.exists():
        shutil.rmtree(blocks_dir)
    blocks_dir.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(args.genesis, args.out / "genesis.json")

    rpc = RpcClient(args.rpc_url)
    if args.capture_file:
        tests, capture_metadata = export_capture(args, rpc, blocks_dir)
    else:
        tests = export_range(args, rpc, blocks_dir)
        capture_metadata = {}

    metadata = {"measurement": "server_execution_ns", "source_rpc": args.rpc_url}
    metadata.update(capture_metadata)
    for entry in args.metadata:
        if "=" not in entry or entry.startswith("="):
            raise ValueError(f"invalid --metadata {entry!r}; expected KEY=VALUE")
        key, value = entry.split("=", 1)
        metadata[key] = value

    manifest = {
        "format": FORMAT,
        "name": args.name,
        "description": args.description,
        "origin": {
            "kind": args.origin_kind,
            "repository": args.origin_repository,
            "revision": args.revision,
            "generator": args.generator,
            "seed": args.seed,
        },
        "chain": {
            "name": "tempo",
            "chain_id": args.chain_id,
            "hardfork": args.hardfork,
            "genesis": "genesis.json",
        },
        "metadata": metadata,
        "defaults": {
            "wait_for_persistence": True,
            "wait_for_caches": args.wait_for_caches,
            "expected_status": "VALID",
        },
        "tests": tests,
    }
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"manifest": str(manifest_path), "tests": len(tests)}, indent=2))


if __name__ == "__main__":
    main()
