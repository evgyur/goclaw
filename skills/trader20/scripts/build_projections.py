#!/usr/bin/env python3
"""Build deterministic GoClaw/Hermes projections from the canonical Trader20 skill."""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
from pathlib import Path

PLATFORMS = ("goclaw", "hermes")
CONTRACT_NAMES = (
    "capabilities.schema.json",
    "execute-plan-request.schema.json",
    "intent-receipt.schema.json",
    "operator-authority-envelope.schema.json",
    "operator-request-receipt.schema.json",
    "plan-trade-request.schema.json",
    "read-envelope.schema.json",
    "risk-policy.schema.json",
    "runtime-snapshot.schema.json",
    "status.schema.json",
    "types.schema.json",
)
CORE_FILES = (
    "skills/trader20/SKILL.md",
    "skills/trader20/references/control-protocol.md",
    "skills/trader20/references/risk-and-authority.md",
    "skills/trader20/references/operator-ux.md",
    "skills/trader20/adapters/goclaw.json",
    "skills/trader20/adapters/hermes.json",
    "skills/trader20/evals/cases.json",
    "skills/trader20/evals/compatibility-fixtures.json",
) + tuple(f"contracts/trader20-control-v1/{name}" for name in CONTRACT_NAMES)


def repo_root() -> Path:
    return Path(__file__).resolve().parents[3]


def hash_core(root: Path) -> str:
    digest = hashlib.sha256()
    for relative in CORE_FILES:
        digest.update(relative.encode("utf-8"))
        digest.update(b"\0")
        digest.update((root / relative).read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()


def build(root: Path, output: Path, source_commit: str) -> dict[str, dict]:
    if len(source_commit) != 40 or any(c not in "0123456789abcdef" for c in source_commit.lower()):
        raise ValueError("source commit must be an exact 40-character hexadecimal SHA")
    core_hash = hash_core(root)
    manifests: dict[str, dict] = {}
    for platform in PLATFORMS:
        target = output / platform
        if target.exists():
            shutil.rmtree(target)
        (target / "references").mkdir(parents=True)
        contract_target = target / "contracts/trader20-control-v1"
        contract_target.mkdir(parents=True)
        shutil.copy2(root / "skills/trader20/SKILL.md", target / "SKILL.md")
        for name in ("control-protocol.md", "risk-and-authority.md", "operator-ux.md"):
            shutil.copy2(root / "skills/trader20/references" / name, target / "references" / name)
        for name in CONTRACT_NAMES:
            shutil.copy2(root / "contracts/trader20-control-v1" / name, contract_target / name)
        shutil.copy2(root / "skills/trader20/adapters" / f"{platform}.json", target / "adapter.json")
        manifest = {
            "schema": "trader20.skill-projection.v1",
            "platform": platform,
            "protocol": "trader20.control.v1",
            "source_commit": source_commit.lower(),
            "core_hash": core_hash,
        }
        (target / "projection-manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        manifests[platform] = manifest
    return manifests


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--source-commit", required=True)
    args = parser.parse_args()
    manifests = build(repo_root(), args.output.resolve(), args.source_commit)
    print(json.dumps(manifests, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
