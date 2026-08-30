from __future__ import annotations

import json
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent


class A04SandboxPolicyTest(unittest.TestCase):
    def setUp(self) -> None:
        self.compose = (HERE / "compose.yaml").read_text()
        self.config = json.loads((HERE / "config.json5").read_text())
        self.policy = json.loads((HERE / "coding-exec-policy.json").read_text())
        self.dockerfile = (HERE / "code-runner/Dockerfile").read_text()

    def test_runner_is_non_root_resource_bounded_and_egress_isolated(self) -> None:
        required = (
            'user: "65532:65532"',
            'read_only: true',
            'security_opt: ["no-new-privileges:true"]',
            'cap_drop: ["ALL"]',
            'pids_limit: 64',
            'mem_limit: 512m',
            'cpus: 0.50',
            'networks: [haraldr-code-runner-internal]',
            'internal: true',
            'restart: "no"',
        )
        runner = self.compose.split("  code-runner:\n", 1)[1].split("\nconfigs:\n", 1)[0]
        for value in required:
            with self.subTest(value=value):
                self.assertIn(value, runner if value != "internal: true" else self.compose)
        self.assertIn("USER 65532:65532", self.dockerfile)

    def test_runner_shares_only_workspace_and_no_credentials_or_host_mounts(self) -> None:
        runner = self.compose.split("  code-runner:\n", 1)[1].split("\nconfigs:\n", 1)[0]
        volume_lines = [line.strip() for line in runner.splitlines() if line.strip().startswith("- haraldr-")]
        self.assertEqual(["- haraldr-goclaw-workspace:/workspace:rw"], volume_lines)
        for forbidden in (
            "/var/run/docker.sock", "/run/containerd/containerd.sock", "/home/hermes",
            "TOKEN", "SECRET", "PASSWORD", "PRIVATE_KEY", "PRIVY", "SIGNER", "WALLET",
        ):
            with self.subTest(forbidden=forbidden):
                self.assertNotIn(forbidden, runner.upper() if forbidden.isupper() else runner)
        self.assertFalse(self.policy["credentials"]["mounted"])
        self.assertFalse(self.policy["credentials"]["environment_inheritance"])
        self.assertEqual(["haraldr-goclaw-workspace:/workspace:rw"], self.policy["container"]["shared_mounts"])

    def test_mcp_surface_and_command_allowlist_are_narrow(self) -> None:
        server = self.config["tools"]["mcp_servers"]["haraldr_code_runner"]
        self.assertEqual("streamable-http", server["transport"])
        self.assertEqual("http://code-runner:8090/mcp", server["url"])
        self.assertIn("coding_exec", self.config["tools"]["allow"])
        self.assertEqual("coding_exec", self.policy["tool"])
        self.assertEqual(["go_test", "go_build", "go_vet"], self.policy["commands"]["allow"])
        denied = set(self.policy["commands"]["deny"])
        self.assertTrue({
            "shell", "package_install", "service_management", "container_management",
            "raw_network_client", "credential_access", "host_path_access",
        }.issubset(denied))

    def test_policy_enforces_relative_paths_and_hard_bounds(self) -> None:
        self.assertEqual("/workspace", self.policy["checkout_root"])
        self.assertIn("no-traversal", self.policy["workdir_policy"])
        self.assertIn("no-symlink-escape", self.policy["workdir_policy"])
        self.assertEqual(120, self.policy["limits"]["timeout_seconds_max"])
        self.assertEqual(65536, self.policy["limits"]["output_bytes_default"])
        self.assertEqual(64, self.policy["limits"]["pids"])
        self.assertFalse(self.policy["network"]["external_egress"])
        self.assertTrue(self.policy["network"]["internal_only"])


if __name__ == "__main__":
    unittest.main()
