#!/usr/bin/env python3
"""Offline verifier for the exact A06 Haraldr GoClaw release package."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import re
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
EXPECTED_COMMIT = "488dfb79ba150a6b0c456627c4633710a785b307"
EXPECTED_TREE = "82253ac93ef5ebd7fd2b487584ef77ae6c986b6f"
READ_ONLY_TOOLS = {
    "trader20_capabilities", "trader20_status", "trader20_positions",
    "trader20_orders", "trader20_history", "trader20_explain_blocker",
    "trader20_runtime_health", "coding_exec",
}
DENIED = {
    "exec", "shell", "publish_skill", "skill_manage", "write_file", "edit",
    "browser", "web_fetch", "web_search", "cron", "delegate", "spawn",
    "message", "image_generation",
}
FORBIDDEN_COMPOSE = {
    "/var/run/docker.sock", "/etc:", "/opt/trader20-v3", "/var/lib/trader20-v3",
    "/home/hermes", "pear-goclaw", "TELEGRAM_BOT_TOKEN",
    "PRIVATE_KEY", "PRIVY", "SIGNER", "WALLET", "TRADING_SECRET",
}


def tree_hash(base: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(item for item in base.rglob("*") if item.is_file()):
        digest.update(path.relative_to(base).as_posix().encode())
        digest.update(b"\0")
        digest.update(path.read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()


def projection_hash(source_commit: str) -> tuple[str, str]:
    module_path = ROOT / "skills/trader20/scripts/build_projections.py"
    spec = importlib.util.spec_from_file_location("trader20_projection", module_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    import tempfile
    with tempfile.TemporaryDirectory() as directory:
        manifests = module.build(ROOT, Path(directory), source_commit)
        return manifests["goclaw"]["core_hash"], tree_hash(Path(directory) / "goclaw")


def load_provision_module():
    module_path = HERE / "provision.py"
    spec = importlib.util.spec_from_file_location("haraldr_goclaw_provision", module_path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def verify() -> dict:
    errors: list[str] = []
    provenance = json.loads((HERE / "provenance.json").read_text())
    policy = json.loads((HERE / "broker-policy.json").read_text())
    config = json.loads((HERE / "config.json5").read_text())
    compose = (HERE / "compose.yaml").read_text()
    provisioned = load_provision_module().payloads()

    head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    source_tree = subprocess.check_output(
        ["git", "rev-parse", f"{EXPECTED_COMMIT}^{{tree}}"], cwd=ROOT, text=True
    ).strip()
    ancestor = subprocess.run(
        ["git", "merge-base", "--is-ancestor", EXPECTED_COMMIT, "HEAD"], cwd=ROOT
    ).returncode == 0
    source_drift = subprocess.run(
        ["git", "diff", "--quiet", EXPECTED_COMMIT, "--", ".", ":(exclude)deploy/haraldr-goclaw"],
        cwd=ROOT,
    ).returncode != 0
    if provenance["source_commit"] != EXPECTED_COMMIT or not ancestor:
        errors.append("pinned source commit mismatch or not an ancestor")
    if source_tree != EXPECTED_TREE or provenance["source_tree"] != EXPECTED_TREE:
        errors.append("pinned source tree mismatch")
    if source_drift:
        errors.append("runtime source differs from pinned candidate outside deploy/haraldr-goclaw")

    core_hash, built_projection_hash = projection_hash(EXPECTED_COMMIT)
    if core_hash != provenance["a02"]["core_hash"]:
        errors.append("A02 core hash mismatch")
    if built_projection_hash != provenance["a02"]["goclaw_projection_hash"]:
        errors.append("A02 GoClaw projection hash mismatch")
    if tree_hash(ROOT / "contracts/trader20-control-v1") != provenance["a02"]["contract_hash"]:
        errors.append("A02 contract hash mismatch")

    tools = config["tools"]
    agent_tools = provisioned["agent"]["tools_config"]
    if set(tools["allow"]) != READ_ONLY_TOOLS or set(agent_tools["allow"]) != READ_ONLY_TOOLS:
        errors.append("effective allowlist is not the exact Trader20 read-only set")
    if tools.get("profile") != "full":
        errors.append("tool profile must begin with the full registry before the explicit allowlist intersection")
    if not DENIED.issubset(set(tools["deny"])) or not DENIED.issubset(set(agent_tools["deny"])):
        errors.append("required tool denylist is incomplete")
    if set(policy["allow_tools"]) != READ_ONLY_TOOLS or policy["effect_scope"] != "repo_mutation":
        errors.append("effective policy is not exact read-only plus bounded coding_exec")
    if config["tools"]["execApproval"] != {"security": "deny", "ask": "off", "allowlist": []}:
        errors.append("native exec approval is not deny/off")
    if config["tools"]["browser"].get("enabled") is not False:
        errors.append("browser is enabled")
    if config["tools"]["web_fetch"].get("allowed_domains") != []:
        errors.append("generic web allowlist is non-empty")

    channel = config["channels"]["telegram"]
    if channel.get("enabled") is not False or channel.get("token") != "" or config["bindings"] != []:
        errors.append("static Telegram configuration is not disabled and credential-free")
    if provisioned["channel"].get("enabled") is not False or provisioned["channel"].get("credentials") != {}:
        errors.append("provisioning channel is not disabled and credential-free")
    agent = provisioned["agent"]
    if (agent["agent_key"], agent["agent_type"], agent["provider"], agent["model"]) != (
        "haraldr-trader20", "predefined", "h20-keys", "h20-gpt"
    ):
        errors.append("agent identity/provider/model mismatch")
    if agent["subagents_config"].get("enabled") is not False:
        errors.append("delegation is enabled")
    if agent["self_evolve"] or agent["skill_evolve"]:
        errors.append("offline read-only scaffold enables evolution")

    for forbidden in FORBIDDEN_COMPOSE:
        if forbidden.lower() in compose.lower():
            errors.append(f"forbidden compose surface: {forbidden}")
    if "127.0.0.1:19972:18790" not in compose:
        errors.append("gateway is not bound to dedicated loopback port")
    for required in (
        "haraldr_goclaw", "haraldr-goclaw-postgres-data", "haraldr-goclaw-data",
        "haraldr-goclaw-workspace", "haraldr-goclaw-internal", "haraldr-code-runner-internal",
        EXPECTED_COMMIT,
    ):
        if required not in compose:
            errors.append(f"missing dedicated/pinned compose value: {required}")
    if re.search(r"image:\s+[^\n]+:latest(?:\s|$)", compose):
        errors.append("mutable latest image tag present")
    if provenance["runtime_image_digest"] is not None:
        errors.append("repository release manifest must leave the runtime digest for the external build receipt")
    if provenance["activation_authorized"] is not True or provenance["telegram_enabled"] is not True:
        errors.append("A06 release package is not authorized for the Telegram cutover")
    if provenance["money_effects_authorized"] is not False:
        errors.append("A06 release package authorizes money effects")
    if "GOCLAW_TELEGRAM_TOKEN" in compose:
        errors.append("Telegram token must be installed through encrypted channel credentials, not container environment")
    if "--enable-telegram" not in (HERE / "provision.py").read_text():
        errors.append("A06 package has no explicit Telegram cutover command")
    if "enable_trader20_tools" not in (HERE / "provision.py").read_text():
        errors.append("A06 package does not enable the disabled-by-default Trader20 builtin tools")
    if 'cap_add: ["CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID"]' not in compose:
        errors.append("PostgreSQL entrypoint cannot initialize its volume and drop privileges")

    if errors:
        raise AssertionError("; ".join(errors))
    return {
        "ok": True,
        "phase": "A06-deploy-cutover",
        "candidate_commit": EXPECTED_COMMIT,
        "candidate_tree": source_tree,
        "scaffold_commit": head,
        "contract_hash": provenance["a02"]["contract_hash"],
        "core_hash": core_hash,
        "goclaw_projection_hash": built_projection_hash,
        "agent": "haraldr-trader20",
        "provider": "h20-keys",
        "model": "h20-gpt",
        "tool_allow_count": len(READ_ONLY_TOOLS),
        "telegram_enabled": True,
        "activation_authorized": True,
    }


def main() -> int:
    print(json.dumps(verify(), sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
