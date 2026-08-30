from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path

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
        self.assertEqual("A06-deploy-cutover", receipt["phase"])
        self.assertEqual(8, receipt["tool_allow_count"])

    def test_provisioning_payload_is_predefined_disabled_and_read_only(self) -> None:
        plans = provision.payloads()
        agent = plans["agent"]
        self.assertEqual("predefined", agent["agent_type"])
        self.assertEqual("h20-keys", agent["provider"])
        self.assertEqual("h20-gpt", agent["model"])
        self.assertEqual(set(verifier.READ_ONLY_TOOLS), set(agent["tools_config"]["allow"]))
        self.assertTrue(verifier.DENIED.issubset(set(agent["tools_config"]["deny"])))
        self.assertFalse(agent["subagents_config"]["enabled"])
        self.assertFalse(agent["self_evolve"])
        self.assertFalse(agent["skill_evolve"])
        self.assertFalse(plans["channel"]["enabled"])
        self.assertEqual({}, plans["channel"]["credentials"])


if __name__ == "__main__":
    unittest.main()
