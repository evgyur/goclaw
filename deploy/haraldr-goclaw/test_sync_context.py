import base64
import hashlib
import importlib.util
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("sync_context.py")
spec = importlib.util.spec_from_file_location("sync_context", MODULE_PATH)
assert spec is not None
assert spec.loader is not None
sync_context = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sync_context)


class HaraldrContextTest(unittest.TestCase):
    def test_context_has_required_architecture_and_boundaries(self):
        files = sync_context.load_context()
        capabilities = files["CAPABILITIES.md"]
        task = files["AGENTS_TASK.md"]
        for required in (
            "Trader20 is the production trading runtime and the only exchange writer",
            "call the relevant Trader20 read tools",
            "up to 240 candidates",
            "cohorts of 60",
            "separate VDSina egress",
            "does not replay stale openings",
            "risk per trade 20 USDC",
            "signing_available=false",
            "DEGRADED_RESERVE` with `entriesBlocked=false",
            "operator-request effect",
        ):
            self.assertIn(required, capabilities)
        self.assertIn("A persisted or acknowledged intent is not a trade", capabilities)
        self.assertIn("Never equate an intent, plan, HTTP response, or order acknowledgement with a fill", task)

    def test_context_contains_no_secret_or_full_wallet_material(self):
        joined = "\n".join(sync_context.load_context().values()).lower()
        for forbidden in ("private key", "api key:", "telegram token:", "0x5372a580", "0x4b8fad9b"):
            self.assertNotIn(forbidden, joined)

    def test_sql_round_trip_payload_is_exact(self):
        files = sync_context.load_context()
        sql = sync_context.build_sql(files)
        self.assertIn("ON CONFLICT (agent_id, file_name) DO UPDATE", sql)
        for name, content in files.items():
            encoded = base64.b64encode(content.encode()).decode()
            self.assertIn(name, sql)
            self.assertIn(encoded, sql)
        self.assertEqual(sync_context.hashes(files), {
            name: hashlib.sha256(content.encode()).hexdigest() for name, content in files.items()
        })

    def test_parse_readback_ignores_non_context_output(self):
        files = sync_context.load_context()
        output = "BEGIN\n" + "\n".join(
            f"{name}|{base64.b64encode(content.encode()).decode()}" for name, content in files.items()
        )
        self.assertEqual(sync_context.parse_readback(output), files)


if __name__ == "__main__":
    unittest.main()
