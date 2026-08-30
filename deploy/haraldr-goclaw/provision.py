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
            "tools_config": {"allow": READ_ONLY_TOOLS, "deny": DENIED_TOOLS},
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


def request(token: str, method: str, path: str, body: dict) -> dict:
    headers = {
        "Authorization": "Bearer " + token,
        "X-GoClaw-User-Id": "system",
        "Content-Type": "application/json",
    }
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(body).encode(),
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
    print(json.dumps({
        "provider_id": created_provider.get("id"),
        "agent_id": created_agent.get("id"),
        "channel_id": created_channel.get("id"),
        "channel_enabled": False,
    }, sort_keys=True))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    if args.apply:
        apply()
    else:
        print(json.dumps(payloads(), indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
