from __future__ import annotations

import copy
import hashlib
import importlib.util
import unittest
from pathlib import Path
from unittest import mock

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("release_broker", HERE / "release_broker.py")
assert SPEC and SPEC.loader
broker = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(broker)

SAFE_PATH = "deploy/haraldr-goclaw/example.txt"
SAFE_PATCH = (
    f"diff --git a/{SAFE_PATH} b/{SAFE_PATH}\n"
    "index 1111111..2222222 100644\n"
    f"--- a/{SAFE_PATH}\n"
    f"+++ b/{SAFE_PATH}\n"
    "@@ -1 +1 @@\n-old\n+safe configuration\n"
)


def valid_request() -> dict:
    return {
        "operation": "publish_candidate_branch",
        "task_id": "A04-offline",
        "repository": "evgyur/goclaw",
        "base_branch": "feat/hyperliquid-readonly-tools-20260830",
        "base_sha": "9cba0fbb915d9c16a7f2f73e05681fc3c33d27e7",
        "candidate_branch": "feat/hyperliquid-readonly-tools-20260830",
        "candidate_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "candidate_tree": "151370e2b542dc62e63d13f56d66c3dea0b19234",
        "idempotency_key": "A04:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "diff_sha256": hashlib.sha256(SAFE_PATCH.encode()).hexdigest(),
        "allowed_write_set": ["deploy/haraldr-goclaw/**"],
        "changed_files": [SAFE_PATH],
        "diff_patch": SAFE_PATCH,
        "force": False,
        "remote_effect_authorized": False,
    }


class ReleaseBrokerPolicyTest(unittest.TestCase):
    def test_valid_offline_candidate_receipt_is_stable(self) -> None:
        request = valid_request()
        first = broker.validate_request(request)
        second = broker.validate_request(copy.deepcopy(request))
        self.assertTrue(first["ok"])
        self.assertFalse(first["remote_effect_authorized"])
        self.assertEqual(first["receipt_id"], second["receipt_id"])
        self.assertEqual(request["candidate_sha"], first["candidate_sha"])

    def assert_rejected(self, **changes) -> None:
        request = valid_request()
        request.update(changes)
        with self.assertRaises(broker.PolicyRejected):
            broker.validate_request(request)

    def test_rejects_main_and_force_pushes(self) -> None:
        self.assert_rejected(candidate_branch="main")
        self.assert_rejected(force=True)
        request = valid_request()
        request.pop("force")
        with self.assertRaises(broker.PolicyRejected):
            broker.validate_request(request)

    def test_rejects_unapproved_repository_and_scope_drift(self) -> None:
        self.assert_rejected(repository="attacker/goclaw")
        path = "cmd/backdoor.go"
        patch = SAFE_PATCH.replace(SAFE_PATH, path)
        self.assert_rejected(
            changed_files=[path],
            diff_patch=patch,
            diff_sha256=hashlib.sha256(patch.encode()).hexdigest(),
        )

    def test_rejects_missing_idempotency_or_candidate_identity(self) -> None:
        for field in ("idempotency_key", "candidate_sha", "candidate_tree", "base_sha", "diff_sha256"):
            request = valid_request()
            request.pop(field)
            with self.subTest(field=field), self.assertRaises(broker.PolicyRejected):
                broker.validate_request(request)
        self.assert_rejected(candidate_sha="short")
        self.assert_rejected(candidate_tree="not-a-tree")
        self.assert_rejected(idempotency_key="short")

    def test_rejects_secret_path_and_secret_content(self) -> None:
        secret_path = "deploy/haraldr-goclaw/.env"
        patch = SAFE_PATCH.replace(SAFE_PATH, secret_path)
        self.assert_rejected(
            changed_files=[secret_path],
            diff_patch=patch,
            diff_sha256=hashlib.sha256(patch.encode()).hexdigest(),
        )
        secret_line = "+api_" + "key = '" + "abcdefghijklmnopqrstuvwxyz123456'"
        secret_patch = SAFE_PATCH.replace("+safe configuration", secret_line)
        self.assert_rejected(
            diff_patch=secret_patch,
            diff_sha256=hashlib.sha256(secret_patch.encode()).hexdigest(),
        )
        pem_marker = "+-----BEGIN " + "PRIVATE KEY-----"
        pem_patch = SAFE_PATCH.replace("+safe configuration", pem_marker)
        self.assert_rejected(
            diff_patch=pem_patch,
            diff_sha256=hashlib.sha256(pem_patch.encode()).hexdigest(),
        )

    def test_rejects_diff_identity_mismatch_and_renames(self) -> None:
        self.assert_rejected(diff_sha256="0" * 64)
        self.assert_rejected(changed_files=["deploy/haraldr-goclaw/other.txt"])
        rename = SAFE_PATCH.replace(
            f"diff --git a/{SAFE_PATH} b/{SAFE_PATH}",
            f"diff --git a/{SAFE_PATH} b/deploy/haraldr-goclaw/renamed.txt",
        )
        self.assert_rejected(diff_patch=rename, diff_sha256=hashlib.sha256(rename.encode()).hexdigest())
        binary = SAFE_PATCH + broker.BINARY_PATCH_MARKER + "\n"
        self.assert_rejected(diff_patch=binary, diff_sha256=hashlib.sha256(binary.encode()).hexdigest())

    def test_submit_is_guarded_before_any_network_call(self) -> None:
        with mock.patch.dict("os.environ", {}, clear=True), mock.patch(
            "urllib.request.urlopen"
        ) as urlopen:
            with self.assertRaises(RuntimeError):
                broker.submit("https://broker.invalid/releases", valid_request())
            urlopen.assert_not_called()


if __name__ == "__main__":
    unittest.main()
