#!/usr/bin/env python3
"""Validate Tempo Engine suite manifests and referenced files."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any, Iterator


FORMAT = "tempo-engine-suite/v1"
FILE_FIELDS = ("rlp_file", "bal_file")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate Tempo suite manifests and referenced block files."
    )
    parser.add_argument(
        "paths",
        nargs="+",
        type=Path,
        help="Suite directories or manifest.json paths to validate.",
    )
    parser.add_argument(
        "--allow-outside-refs",
        action="store_true",
        help="Allow file references that point outside the suite directory.",
    )
    return parser.parse_args()


def manifest_path(path: Path) -> Path:
    return path / "manifest.json" if path.is_dir() else path


def load_manifest(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as source:
        return json.load(source)


def iter_calls(manifest: dict[str, Any]) -> Iterator[tuple[str, str, dict[str, Any]]]:
    for call in manifest.get("pre_run", []):
        yield "pre_run", "<suite>", call
    for test in manifest.get("tests", []):
        test_name = test.get("name", "<unnamed>")
        for phase in ("setup", "test", "cleanup"):
            for call in test.get(phase, []):
                yield phase, test_name, call


def is_inside(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
        return True
    except ValueError:
        return False


def validate(path: Path, allow_outside_refs: bool) -> int:
    path = manifest_path(path)
    errors: list[str] = []
    if not path.is_file():
        errors.append("manifest does not exist")
        print_result(path, 0, 0, errors)
        return len(errors)

    try:
        manifest = load_manifest(path)
    except json.JSONDecodeError as error:
        errors.append(f"invalid JSON: {error}")
        print_result(path, 0, 0, errors)
        return len(errors)

    if manifest.get("format") != FORMAT:
        errors.append(
            f"unsupported format {manifest.get('format')!r}, expected {FORMAT!r}"
        )
    if not manifest.get("name"):
        errors.append("missing name")
    if not manifest.get("tests"):
        errors.append("missing tests")

    genesis_ref = manifest.get("chain", {}).get("genesis")
    if not genesis_ref:
        errors.append("missing chain.genesis")
    elif not (path.parent / genesis_ref).is_file():
        errors.append(f"missing genesis file {genesis_ref}")

    outside = 0
    suite_root = path.parent.resolve()
    for phase, test_name, call in iter_calls(manifest):
        for field in FILE_FIELDS:
            ref = call.get(field)
            if not ref:
                continue
            ref_path = (path.parent / ref).resolve()
            if not is_inside(ref_path, suite_root):
                outside += 1
                if not allow_outside_refs:
                    errors.append(
                        f"{test_name} {phase}: {field} points outside suite: {ref}"
                    )
            if not ref_path.is_file():
                errors.append(f"{test_name} {phase}: missing {field} file {ref}")

    print_result(path, len(manifest.get("tests", [])), outside, errors)
    return len(errors)


def print_result(path: Path, tests: int, outside: int, errors: list[str]) -> None:
    status = "ok" if not errors else "failed"
    print(
        f"{path}: {status}; tests={tests}; "
        f"outside_refs={outside}; errors={len(errors)}"
    )
    for error in errors:
        print(f"  - {error}")


def main() -> None:
    args = parse_args()
    total_errors = 0
    for path in args.paths:
        total_errors += validate(path, args.allow_outside_refs)
    if total_errors:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
