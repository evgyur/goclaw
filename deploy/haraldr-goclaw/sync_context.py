#!/usr/bin/env python3
"""Idempotently install Haraldr's source-controlled context into its GoClaw DB."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
from pathlib import Path
import subprocess
import sys

AGENT_KEY = "haraldr-trader20"
ALLOWED_FILES = ("AGENTS_TASK.md", "CAPABILITIES.md", "IDENTITY.md", "USER_PREDEFINED.md")
ROOT = Path(__file__).resolve().parent
CONTEXT_DIR = ROOT / "context"


def load_context() -> dict[str, str]:
    files: dict[str, str] = {}
    for name in ALLOWED_FILES:
        text = (CONTEXT_DIR / name).read_text(encoding="utf-8")
        if not text.strip():
            raise RuntimeError(f"empty context file: {name}")
        files[name] = text
    return files


def hashes(files: dict[str, str]) -> dict[str, str]:
    return {name: hashlib.sha256(text.encode()).hexdigest() for name, text in files.items()}


def sql_literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def build_sql(files: dict[str, str]) -> str:
    values = []
    for name, text in files.items():
        encoded = base64.b64encode(text.encode()).decode()
        values.append(f"({sql_literal(name)}, convert_from(decode({sql_literal(encoded)}, 'base64'), 'UTF8'))")
    return rf"""\set ON_ERROR_STOP on
BEGIN;
DO $$
DECLARE n integer;
BEGIN
  SELECT count(*) INTO n FROM agents WHERE agent_key = {sql_literal(AGENT_KEY)} AND deleted_at IS NULL;
  IF n <> 1 THEN RAISE EXCEPTION 'expected exactly one active {AGENT_KEY} agent, found %', n; END IF;
END $$;
WITH target AS (
  SELECT id, tenant_id FROM agents WHERE agent_key = {sql_literal(AGENT_KEY)} AND deleted_at IS NULL
), incoming(file_name, content) AS (
  VALUES {', '.join(values)}
)
INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT gen_random_uuid(), target.id, incoming.file_name, incoming.content, target.tenant_id, now(), now()
FROM target CROSS JOIN incoming
ON CONFLICT (agent_id, file_name) DO UPDATE
SET content = EXCLUDED.content, updated_at = now();
COMMIT;
SELECT file_name || '|' || replace(encode(convert_to(content, 'UTF8'), 'base64'), chr(10), '')
FROM agent_context_files ac
JOIN agents a ON a.id = ac.agent_id
WHERE a.agent_key = {sql_literal(AGENT_KEY)} AND ac.file_name IN ({', '.join(sql_literal(n) for n in ALLOWED_FILES)})
ORDER BY file_name;
"""


def run_psql(container: str, database: str, user: str, sql: str) -> str:
    proc = subprocess.run(
        ["docker", "exec", "-i", container, "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", database, "-At"],
        input=sql,
        text=True,
        capture_output=True,
        check=False,
    )
    if proc.returncode:
        raise RuntimeError(proc.stderr.strip() or f"psql exited {proc.returncode}")
    return proc.stdout


def parse_readback(output: str) -> dict[str, str]:
    observed: dict[str, str] = {}
    for line in output.splitlines():
        if "|" not in line:
            continue
        name, encoded = line.split("|", 1)
        if name not in ALLOWED_FILES:
            continue
        observed[name] = base64.b64decode(encoded).decode()
    return observed


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--container", default="haraldr-goclaw-postgres-1")
    parser.add_argument("--database", default="haraldr_goclaw")
    parser.add_argument("--user", default="haraldr_goclaw")
    args = parser.parse_args()

    expected = load_context()
    expected_hashes = hashes(expected)
    if not args.apply:
        print(json.dumps({"agent_key": AGENT_KEY, "files": expected_hashes, "apply": False}, sort_keys=True))
        return 0

    output = run_psql(args.container, args.database, args.user, build_sql(expected))
    observed = parse_readback(output)
    observed_hashes = hashes(observed)
    if observed_hashes != expected_hashes:
        raise RuntimeError(f"context readback mismatch: expected={expected_hashes} observed={observed_hashes}")
    print(json.dumps({"agent_key": AGENT_KEY, "files": observed_hashes, "apply": True, "verified": True}, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
