#!/usr/bin/env python3
"""Render or explicitly apply the A03 Haraldr GoClaw read-only provisioning payload."""

from __future__ import annotations

import argparse
import json
import os
import urllib.request

BASE = "http://127.0.0.1:19972"
READ_ONLY_TOOLS = [
    "trader20_capabilities",
    "trader20_status",
    "trader20_positions",
    "trader20_orders",
    "trader20_history",
    "trader20_explain_blocker",
    "trader20_runtime_health",
    "coding_exec",
]
TRADER20_BUILTIN_TOOLS = READ_ONLY_TOOLS[:-1]
DENIED_TOOLS = [
    "exec", "shell", "publish_skill", "skill_manage", "write_file", "edit",
    "browser", "web_fetch", "web_search", "cron", "delegate", "spawn",
    "message", "image_generation",
]


def payloads() -> dict[str, dict]:
    return {
        "provider": {
            "name": "h20-keys",
            "provider_type": "openai_compat",
            "api_key": "${H20_KEYS_API_KEY}",
            "api_base": "https://keys.human20.app/v1",
            "enabled": True,
        },
        "agent": {
            "agent_key": "haraldr-trader20",
            "display_name": "Haraldr Trader20",
            "agent_type": "predefined",
            "provider": "h20-keys",
            "model": "h20-gpt",
            "is_default": True,
            "max_tool_iterations": 8,
            "context_window": 131072,
            "max_tokens": 4096,
            # system_configs may retain a restrictive global profile from an
            # earlier install. Keep the exact per-agent allow list and add the
            # same bounded set after global-profile filtering so this agent
            # cannot silently lose all of its explicitly granted tools.
            "tools_config": {
                "allow": READ_ONLY_TOOLS,
                "alsoAllow": READ_ONLY_TOOLS,
                "deny": DENIED_TOOLS,
            },
            "memory_config": {"enabled": True},
            "subagents_config": {"enabled": False},
            "self_evolve": False,
            "skill_evolve": False,
            "other_config": {"allow_image_generation": False},
        },
        "channel": {
            "name": "haraldr-trader20-telegram",
            "display_name": "Haraldr Trader20 Telegram",
            "channel_type": "telegram",
            "enabled": False,
            "credentials": {},
            "config": {
                "dm_policy": "disabled",
                "group_policy": "disabled",
                "allow_from": [],
                "require_mention": True,
                "dm_stream": False,
                "group_stream": False,
                "reaction_level": "off",
                "link_preview": False,
            },
        },
    }


def request(token: str, method: str, path: str, body: dict | None = None) -> dict:
    headers = {
        "Authorization": "Bearer " + token,
        "X-GoClaw-User-Id": "system",
        "Content-Type": "application/json",
    }
    req = urllib.request.Request(
        BASE + path,
        data=None if body is None else json.dumps(body).encode(),
        method=method,
        headers=headers,
    )
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.load(response)


def apply() -> None:
    if os.environ.get("HARALDR_GOCLAW_ALLOW_APPLY") != "YES":
        raise RuntimeError("apply is blocked; set HARALDR_GOCLAW_ALLOW_APPLY=YES only in an approved online provisioning step")
    token = os.environ["HARALDR_GOCLAW_GATEWAY_TOKEN"]
    api_key = os.environ["H20_KEYS_API_KEY"]
    plans = payloads()
    provider = dict(plans["provider"], api_key=api_key)
    created_provider = request(token, "POST", "/v1/providers", provider)
    created_agent = request(token, "POST", "/v1/agents", plans["agent"])
    channel = dict(plans["channel"], agent_id=created_agent["id"])
    created_channel = request(token, "POST", "/v1/channels/instances", channel)
    if created_channel.get("enabled") is not False:
        raise RuntimeError("Telegram channel readback is not disabled")
    enabled_tools = enable_trader20_tools(token)
    print(json.dumps({
        "provider_id": created_provider.get("id"),
        "agent_id": created_agent.get("id"),
        "channel_id": created_channel.get("id"),
        "channel_enabled": False,
        "enabled_builtin_tools": enabled_tools,
    }, sort_keys=True))


def enable_trader20_tools(token: str) -> list[str]:
    enabled = []
    for name in TRADER20_BUILTIN_TOOLS:
        request(token, "PUT", f"/v1/tools/builtin/{name}", {"enabled": True})
        readback = request(token, "GET", f"/v1/tools/builtin/{name}")
        if readback.get("name") != name or readback.get("enabled") is not True:
            raise RuntimeError(f"builtin tool enable readback failed: {name}")
        enabled.append(name)
    return enabled


def set_telegram_enabled(enabled: bool) -> None:
    if os.environ.get("HARALDR_GOCLAW_ALLOW_TELEGRAM_CUTOVER") != "YES":
        raise RuntimeError("Telegram cutover is blocked; set HARALDR_GOCLAW_ALLOW_TELEGRAM_CUTOVER=YES only in an approved one-owner cutover")
    gateway_token = os.environ["HARALDR_GOCLAW_GATEWAY_TOKEN"]
    listed = request(gateway_token, "GET", "/v1/channels/instances")
    channels = [item for item in listed.get("instances", []) if item.get("name") == "haraldr-trader20-telegram"]
    if len(channels) != 1:
        raise RuntimeError(f"expected exactly one Haraldr Telegram channel, found {len(channels)}")
    channel = channels[0]
    updates: dict[str, object] = {"enabled": enabled}
    if enabled:
        updates.update({
            "credentials": {"token": os.environ["GOCLAW_TELEGRAM_TOKEN"]},
            "config": {
                "dm_policy": "allowlist",
                "group_policy": "disabled",
                "allow_from": ["617744661"],
                "require_mention": True,
                "dm_stream": False,
                "group_stream": False,
                "reaction_level": "off",
                "link_preview": False,
            },
        })
    request(gateway_token, "PUT", f"/v1/channels/instances/{channel['id']}", updates)
    readback = request(gateway_token, "GET", f"/v1/channels/instances/{channel['id']}")
    if readback.get("enabled") is not enabled:
        raise RuntimeError("Telegram channel readback does not match requested state")
    if enabled:
        config = readback.get("config") or {}
        if config.get("dm_policy") != "allowlist" or config.get("allow_from") != ["617744661"]:
            raise RuntimeError("Telegram channel readback is not owner-only")
        if readback.get("has_credentials") is not True:
            raise RuntimeError("Telegram channel readback has no encrypted credentials")
    print(json.dumps({
        "channel_id": channel["id"],
        "enabled": enabled,
        "owner_allowlist": ["617744661"] if enabled else None,
        "has_credentials": readback.get("has_credentials"),
    }, sort_keys=True))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--enable-telegram", action="store_true")
    parser.add_argument("--disable-telegram", action="store_true")
    args = parser.parse_args()
    if sum((args.apply, args.enable_telegram, args.disable_telegram)) > 1:
        parser.error("choose exactly one state-changing action")
    if args.apply:
        apply()
    elif args.enable_telegram:
        set_telegram_enabled(True)
    elif args.disable_telegram:
        set_telegram_enabled(False)
    else:
        print(json.dumps(payloads(), indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
