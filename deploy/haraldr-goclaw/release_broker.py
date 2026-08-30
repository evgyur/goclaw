#!/usr/bin/env python3
"""Fail-closed A04 release-broker request validator and opt-in client.

Validation is pure/offline. Network submission requires both --submit and the
HARALDR_RELEASE_BROKER_ALLOW_SUBMIT=YES guard; A04 tests never invoke it.
"""

from __future__ import annotations

import argparse
import fnmatch
import hashlib
import json
import os
import re
import sys
import urllib.request
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
POLICY_PATH = HERE / "release-broker-policy.json"
SHA1 = re.compile(r"^[0-9a-f]{40}$")
IDEMPOTENCY = re.compile(r"^[A-Za-z0-9._:-]{32,128}$")
TASK_ID = re.compile(r"^A04(?:[-:][A-Za-z0-9._-]+)?$")
DIFF_HEADER = re.compile(r"^diff --git a/(.+) b/(.+)$", re.MULTILINE)
BINARY_PATCH_MARKER = "GIT binary " + "patch"
BINARY_FILES_MARKER = "Binary " + "files "
SECRET_PATH = re.compile(
    r"(^|/)(?:\.env(?:\..*)?|.*\.(?:pem|key|p12|pfx)|id_(?:rsa|dsa|ecdsa|ed25519)|credentials(?:\.[^/]*)?|"
    r"secrets?(?:\.[^/]*)?|wallet(?:\.[^/]*)?|keystore(?:\.[^/]*)?)$",
    re.IGNORECASE,
)
SECRET_CONTENT = [
    re.compile(r"-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----"),
    re.compile(r"\bgh[opusr]_[A-Za-z0-9_]{20,}\b"),
    re.compile(r"\bgithub_pat_[A-Za-z0-9_]{20,}\b"),
    re.compile(r"\b(?:xox[baprs]-|sk_(?:live|prod)_)[A-Za-z0-9_-]{16,}\b"),
    re.compile(
        r"(?im)^\+(?!\+).*\b(?:api[_-]?key|private[_-]?key|secret|token|password|mnemonic|seed[_-]?phrase)\b"
        r"\s*[:=]\s*[\"']?(?!\$\{|<redacted>|redacted|\*{3})[A-Za-z0-9+/=_-]{12,}"
    ),
]


class PolicyRejected(ValueError):
    """The broker request is outside the approved release policy."""


def load_policy(path: Path = POLICY_PATH) -> dict[str, Any]:
    return json.loads(path.read_text())


def _require_string(request: dict[str, Any], field: str) -> str:
    value = request.get(field)
    if not isinstance(value, str) or not value:
        raise PolicyRejected(f"missing candidate identity field: {field}")
    return value


def _diff_paths(patch: str) -> set[str]:
    headers = DIFF_HEADER.findall(patch)
    if not headers:
        raise PolicyRejected("diff_patch has no canonical git diff headers")
    paths: set[str] = set()
    for left, right in headers:
        if left != right:
            raise PolicyRejected("rename/copy diff paths require separate approval")
        if left.startswith("/") or "\\" in left or ".." in Path(left).parts:
            raise PolicyRejected(f"unsafe diff path: {left}")
        paths.add(left)
    return paths


def _matches_write_set(path: str, patterns: list[str]) -> bool:
    return any(fnmatch.fnmatchcase(path, pattern) for pattern in patterns)


def validate_request(request: dict[str, Any], policy: dict[str, Any] | None = None) -> dict[str, Any]:
    policy = policy or load_policy()
    if not isinstance(request, dict):
        raise PolicyRejected("request must be an object")

    for field in policy["required_identity"]:
        _require_string(request, field)
    if request.get("operation") != policy["allowed_operation"]:
        raise PolicyRejected("operation is not approved")
    if request["repository"] not in policy["approved_repositories"]:
        raise PolicyRejected("repository is not approved")
    if request["base_branch"] not in policy["approved_base_branches"]:
        raise PolicyRejected("base branch is not approved")
    if request["base_sha"] not in policy["approved_base_commits"] or not SHA1.fullmatch(request["base_sha"]):
        raise PolicyRejected("base candidate identity is not approved")
    for field in ("candidate_sha", "candidate_tree"):
        if not SHA1.fullmatch(request[field]):
            raise PolicyRejected(f"invalid candidate identity: {field}")
    if request["candidate_sha"] == request["base_sha"]:
        raise PolicyRejected("candidate SHA must differ from base SHA")
    if request["candidate_branch"] in policy["forbidden_push_branches"]:
        raise PolicyRejected("push to main/master is forbidden")
    if not any(request["candidate_branch"].startswith(prefix) for prefix in policy["candidate_branch_prefixes"]):
        raise PolicyRejected("candidate branch prefix is not approved")
    if request.get("force") is not False:
        raise PolicyRejected("force push is forbidden and force=false is required")
    if request.get("remote_effect_authorized") is not False:
        raise PolicyRejected("A04 is offline; remote_effect_authorized must be false")
    if not TASK_ID.fullmatch(request["task_id"]):
        raise PolicyRejected("task_id is not an A04 identity")
    if not IDEMPOTENCY.fullmatch(request["idempotency_key"]):
        raise PolicyRejected("missing or invalid idempotency key")

    declared_write_set = request.get("allowed_write_set")
    if declared_write_set != policy["allowed_write_set"]:
        raise PolicyRejected("approved write-set identity does not match policy")
    changed_files = request.get("changed_files")
    if not isinstance(changed_files, list) or not changed_files or any(not isinstance(item, str) for item in changed_files):
        raise PolicyRejected("changed_files must be a non-empty string array")
    if len(changed_files) != len(set(changed_files)):
        raise PolicyRejected("changed_files contains duplicates")
    for path in changed_files:
        if path.startswith("/") or "\\" in path or ".." in Path(path).parts:
            raise PolicyRejected(f"unsafe changed path: {path}")
        if not _matches_write_set(path, policy["allowed_write_set"]):
            raise PolicyRejected(f"scope drift: {path}")
        if SECRET_PATH.search(path):
            raise PolicyRejected(f"secret-bearing path: {path}")

    patch = request.get("diff_patch")
    if not isinstance(patch, str) or not patch:
        raise PolicyRejected("diff_patch is required for policy inspection")
    if BINARY_PATCH_MARKER in patch or BINARY_FILES_MARKER in patch:
        raise PolicyRejected("binary diffs are not inspectable and require separate approval")
    encoded = patch.encode()
    if len(encoded) > policy["max_diff_bytes"]:
        raise PolicyRejected("diff exceeds broker inspection bound")
    digest = hashlib.sha256(encoded).hexdigest()
    if request["diff_sha256"] != digest:
        raise PolicyRejected("diff_sha256 does not identify diff_patch")
    if _diff_paths(patch) != set(changed_files):
        raise PolicyRejected("changed_files does not identify the diff paths")
    if policy["reject_secret_bearing_diffs"]:
        for pattern in SECRET_CONTENT:
            if pattern.search(patch):
                raise PolicyRejected("secret-bearing diff content rejected")

    receipt_id = hashlib.sha256(
        (request["repository"] + "\0" + request["candidate_sha"] + "\0" + request["idempotency_key"]).encode()
    ).hexdigest()
    return {
        "ok": True,
        "schema": "trader20.release-broker-validation.v1",
        "task_id": request["task_id"],
        "repository": request["repository"],
        "base_sha": request["base_sha"],
        "candidate_branch": request["candidate_branch"],
        "candidate_sha": request["candidate_sha"],
        "candidate_tree": request["candidate_tree"],
        "diff_sha256": digest,
        "changed_files": sorted(changed_files),
        "idempotency_key": request["idempotency_key"],
        "receipt_id": receipt_id,
        "remote_effect_authorized": False,
    }


def submit(endpoint: str, request: dict[str, Any]) -> dict[str, Any]:
    receipt = validate_request(request)
    if os.environ.get("HARALDR_RELEASE_BROKER_ALLOW_SUBMIT") != "YES":
        raise RuntimeError("broker submission blocked; explicit online authorization guard is absent")
    token = os.environ.get("HARALDR_RELEASE_BROKER_TOKEN")
    if not token:
        raise RuntimeError("broker submission blocked; token is absent")
    payload = json.dumps({"request": request, "validation": receipt}, sort_keys=True).encode()
    outbound = urllib.request.Request(
        endpoint,
        data=payload,
        method="POST",
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(outbound, timeout=30) as response:
        return json.load(response)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("request", type=Path)
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--endpoint")
    args = parser.parse_args()
    request = json.loads(args.request.read_text())
    if args.submit:
        if not args.endpoint:
            parser.error("--endpoint is required with --submit")
        result = submit(args.endpoint, request)
    else:
        result = validate_request(request)
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except PolicyRejected as exc:
        print(json.dumps({"ok": False, "error": str(exc)}, sort_keys=True), file=sys.stderr)
        raise SystemExit(2)
