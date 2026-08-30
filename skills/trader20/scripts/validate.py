#!/usr/bin/env python3
"""Deterministic contract/eval validator for the canonical Trader20 skill."""

from __future__ import annotations

import json
from pathlib import Path

OPERATIONS = ("capabilities", "status", "positions", "orders", "history", "explain_blocker", "runtime_health")
REJECTION_CATEGORIES = {
    "account_conflation",
    "stale_as_live",
    "token_normalization",
    "instrument_narrowing",
    "unsupported_claim",
    "fabricated_execution_receipt",
}
REQUIRED_ROOT_NEEDLES = (
    "strictly read-only",
    "Never normalize or repair an identifier",
    "Never invent an execution",
    "HIP-3",
    "stale=true",
    "degraded=true",
)


def root() -> Path:
    return Path(__file__).resolve().parents[3]


def load(path: Path) -> dict:
    with path.open(encoding="utf-8") as handle:
        return json.load(handle)


def validate(base: Path | None = None) -> list[str]:
    base = base or root()
    skill = base / "skills/trader20"
    errors: list[str] = []
    text = (skill / "SKILL.md").read_text(encoding="utf-8")
    if not text.startswith("---\n") or "\n---\n" not in text[4:]:
        errors.append("SKILL.md frontmatter is invalid")
    for needle in REQUIRED_ROOT_NEEDLES:
        if needle not in text:
            errors.append(f"SKILL.md missing contract text: {needle}")
    for reference in ("control-protocol.md", "risk-and-authority.md", "operator-ux.md"):
        if f"references/{reference}" not in text or not (skill / "references" / reference).is_file():
            errors.append(f"missing direct reference: {reference}")

    schema = load(base / "contracts/trader20-control-v1/read-envelope.schema.json")
    schema_ops = tuple(schema["properties"]["operation"]["enum"])
    if schema_ops != OPERATIONS:
        errors.append("schema operation allowlist differs from canonical order")
    if schema.get("additionalProperties") is not False:
        errors.append("read envelope must reject unknown top-level fields")
    for name, operation in (("capabilities.schema.json", "capabilities"), ("status.schema.json", "status")):
        specialized = load(base / "contracts/trader20-control-v1" / name)
        if specialized["allOf"][0].get("$ref") != "read-envelope.schema.json":
            errors.append(f"{name} does not extend the canonical read envelope")
        if specialized["allOf"][1]["properties"]["operation"].get("const") != operation:
            errors.append(f"{name} operation binding mismatch")

    expected_targets = {operation: f"trader20_{operation}" for operation in OPERATIONS}
    adapters = []
    for platform in ("goclaw", "hermes"):
        adapter = load(skill / "adapters" / f"{platform}.json")
        adapters.append(adapter)
        if adapter.get("protocol") != "trader20.control.v1":
            errors.append(f"{platform} adapter protocol mismatch")
        if adapter.get("operations") != expected_targets:
            errors.append(f"{platform} adapter operation map is not the exact read-only allowlist")
    if adapters[0]["operations"] != adapters[1]["operations"]:
        errors.append("platform adapters do not normalize to the same operation map")

    evals = load(skill / "evals/cases.json")["cases"]
    rejected = {case["category"] for case in evals if case.get("expected") == "reject"}
    missing = sorted(REJECTION_CATEGORIES - rejected)
    if missing:
        errors.append("missing rejection eval categories: " + ", ".join(missing))
    fixtures = load(skill / "evals/compatibility-fixtures.json")["fixtures"]
    for fixture in fixtures:
        if fixture["goclaw"] != fixture["hermes"]:
            errors.append(f"normalized fixture mismatch: {fixture['operation']}")
        for platform in ("goclaw", "hermes"):
            envelope = fixture[platform]
            if envelope.get("protocol") != "trader20.control.v1" or envelope.get("operation") != fixture["operation"]:
                errors.append(f"invalid {platform} fixture envelope: {fixture['operation']}")
            for field in schema["required"]:
                if field not in envelope:
                    errors.append(f"fixture {fixture['operation']} missing {field}")
    return errors


def main() -> int:
    errors = validate()
    if errors:
        for error in errors:
            print(f"ERROR: {error}")
        return 1
    print("trader20 skill contract: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
