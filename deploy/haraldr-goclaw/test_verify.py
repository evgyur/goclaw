from __future__ import annotations

import importlib.util
import json
import os
import unittest
from pathlib import Path
from unittest import mock

HERE = Path(__file__).resolve().parent


def load(name: str):
    path = HERE / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


provision = load("provision")
verifier = load("verify")


class HaraldrGoClawReleaseTest(unittest.TestCase):
    def test_a06_release_policy_and_hashes(self) -> None:
        receipt = verifier.verify()
        self.assertTrue(receipt["ok"])
        self.assertTrue(receipt["telegram_enabled"])
        self.assertTrue(receipt["activation_authorized"])
        self.assertEqual("B02-bounded-control-projections", receipt["phase"])
        self.assertEqual(10, receipt["tool_allow_count"])
        self.assertEqual("full", json.loads((HERE / "config.json5").read_text())["tools"]["profile"])

    def test_provisioning_payload_is_predefined_disabled_and_read_only(self) -> None:
        plans = provision.payloads()
        agent = plans["agent"]
        self.assertEqual("predefined", agent["agent_type"])
        self.assertEqual("h20-keys", agent["provider"])
        self.assertEqual("h20-gpt", agent["model"])
        self.assertEqual(set(verifier.BOUNDED_TOOLS), set(agent["tools_config"]["allow"]))
        self.assertEqual(set(verifier.BOUNDED_TOOLS), set(agent["tools_config"]["alsoAllow"]))
        self.assertTrue(verifier.DENIED.issubset(set(agent["tools_config"]["deny"])))
        self.assertFalse(agent["subagents_config"]["enabled"])
        self.assertFalse(agent["self_evolve"])
        self.assertFalse(agent["skill_evolve"])
        self.assertFalse(plans["channel"]["enabled"])
        self.assertEqual({}, plans["channel"]["credentials"])

    def test_builtin_read_only_tools_are_enabled_with_exact_readback(self) -> None:
        calls = []

        def fake_request(_token, method, path, body=None):
            calls.append((method, path, body))
            name = path.rsplit("/", 1)[-1]
            if method == "GET":
                return {"name": name, "enabled": True}
            return {"status": "updated"}

        with mock.patch.object(provision, "request", side_effect=fake_request):
            enabled = provision.enable_trader20_tools("gateway-test-token")
        self.assertEqual(provision.TRADER20_BUILTIN_TOOLS, enabled)
        for name in provision.TRADER20_BUILTIN_TOOLS:
            self.assertIn(("PUT", f"/v1/tools/builtin/{name}", {"enabled": True}), calls)
            self.assertIn(("GET", f"/v1/tools/builtin/{name}", None), calls)

    def test_telegram_cutover_is_owner_only_and_requires_explicit_authority(self) -> None:
        with self.assertRaises(RuntimeError):
            provision.set_telegram_enabled(True)
        calls = []

        def fake_request(_token, method, path, body=None):
            calls.append((method, path, body))
            if method == "GET" and path == "/v1/channels/instances":
                return {"instances": [{"id": "channel-1", "name": "haraldr-trader20-telegram"}]}
            if method == "GET":
                return {
                    "enabled": True,
                    "config": {"dm_policy": "allowlist", "allow_from": ["617744661"]},
                    "has_credentials": True,
                }
            return {"status": "updated"}

        env = {
            "HARALDR_GOCLAW_ALLOW_TELEGRAM_CUTOVER": "YES",
            "HARALDR_GOCLAW_GATEWAY_TOKEN": "gateway-test-token",
            "GOCLAW_TELEGRAM_TOKEN": "telegram-test-token",
        }
        with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(provision, "request", side_effect=fake_request):
            provision.set_telegram_enabled(True)
        update = next(body for method, _path, body in calls if method == "PUT")
        self.assertEqual(["617744661"], update["config"]["allow_from"])
        self.assertEqual("allowlist", update["config"]["dm_policy"])
        self.assertEqual("disabled", update["config"]["group_policy"])
        self.assertEqual({"token": "telegram-test-token"}, update["credentials"])


if __name__ == "__main__":
    unittest.main()
