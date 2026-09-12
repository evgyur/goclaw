import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

MODULE = Path(__file__).with_name("trader20_control_broker.py")
spec = importlib.util.spec_from_file_location("broker", MODULE)
broker = importlib.util.module_from_spec(spec)
assert spec and spec.loader
spec.loader.exec_module(broker)


class BrokerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        broker.STATE_ROOT = root / "state"
        broker.CONTROL_STATE = broker.STATE_ROOT / "haraldr-control/state.json"
        broker.KILL_SENTINEL = broker.STATE_ROOT / "haraldr-control/kill-latched"
        broker.RUNTIME_KILL_SENTINEL = root / "run/kill-attempted"
        broker.kill_attempted = False
        broker.ACTIVATION = broker.STATE_ROOT / "activation/trader20-v3-active.json"
        broker.WS = broker.STATE_ROOT / "ws-shadow/signals_raw.json"
        broker.ROSTER = broker.STATE_ROOT / "leader-rotation/leader_roster_latest.json"
        broker.WATCH_STATE = broker.STATE_ROOT / "operator-receipts/copy-readiness-watch-state.json"
        broker.HOLD_ENV = root / "haraldr-control.env"
        broker.CURRENT = root / "current"
        release = root / ("release-" + "a" * 12)
        release.mkdir()
        broker.atomic_json(release / "release.json", {"candidateSha": "a" * 40})
        broker.CURRENT.symlink_to(release)
        broker.atomic_json(broker.ACTIVATION, {"status": "ACTIVE", "candidate_sha": "a" * 40, "release": str(release)})
        broker.atomic_json(broker.WS, {"producer_heartbeat_ms": broker.now_ms(), "complete_leader_count": 6, "entries_halted": False})
        broker.save_state({"kill_latched": False, "entries_paused": False, "kill_reason": None, "pause_reason": None})
        self.env = patch.dict("os.environ", {"TRADER20_OPERATOR_USER_ID": "617744661"})
        self.env.start()
        self.services = patch.object(broker, "service_active", return_value=True)
        self.services.start()

    def tearDown(self):
        self.services.stop()
        self.env.stop()
        self.tmp.cleanup()

    def test_unauthorized_actor_cannot_mutate(self):
        with self.assertRaises(PermissionError):
            broker.operational("pause_entries", {"reason": "x"}, "1")
        self.assertFalse(broker.HOLD_ENV.exists())

    def test_pause_resume_and_kill_are_durable_and_fail_closed(self):
        paused = broker.operational("pause_entries", {"reason": "owner_request"}, "617744661")
        self.assertEqual(paused["state"], "ENTRIES_PAUSED")
        self.assertIn("=1", broker.HOLD_ENV.read_text())
        resumed = broker.operational("resume_entries", {}, "617744661")
        self.assertEqual(resumed["state"], "ENTRIES_ENABLED")
        self.assertIn("=0", broker.HOLD_ENV.read_text())
        killed = broker.operational("latch_kill", {"reason": "owner_request"}, "617744661")
        self.assertEqual(killed["state"], "KILL_LATCHED")
        with self.assertRaisesRegex(RuntimeError, "kill_latched"):
            broker.operational("resume_entries", {}, "617744661")
        self.assertIn("=1", broker.HOLD_ENV.read_text())

    def test_manual_money_lane_is_not_spoofed(self):
        for operation in ("plan_trade", "execute_plan"):
            with self.assertRaisesRegex(RuntimeError, "candidate_bound_authority"):
                broker.operational(operation, {}, "617744661")

    def test_runtime_drift_blocks_resume(self):
        broker.operational("pause_entries", {"reason": "test"}, "617744661")
        broker.atomic_json(broker.WS, {"producer_heartbeat_ms": broker.now_ms(), "complete_leader_count": 5, "entries_halted": False})
        with self.assertRaisesRegex(RuntimeError, "websocket_not_exact_six"):
            broker.operational("resume_entries", {}, "617744661")
        self.assertIn("=1", broker.HOLD_ENV.read_text())

    def test_missing_or_corrupt_state_never_releases_hold(self):
        broker.atomic_hold(True)
        broker.CONTROL_STATE.unlink()
        with self.assertRaisesRegex(RuntimeError, "kill_latched"):
            broker.operational("resume_entries", {}, "617744661")
        self.assertIn("=1", broker.HOLD_ENV.read_text())
        broker.CONTROL_STATE.write_text("not-json")
        with self.assertRaisesRegex(RuntimeError, "kill_latched"):
            broker.operational("resume_entries", {}, "617744661")

    def test_kill_sentinel_survives_state_loss(self):
        broker.operational("latch_kill", {"reason": "owner_request"}, "617744661")
        broker.CONTROL_STATE.unlink()
        self.assertTrue(broker.state()["kill_latched"])
        with self.assertRaisesRegex(RuntimeError, "kill_latched"):
            broker.operational("resume_entries", {}, "617744661")

    def test_failed_first_kill_write_latches_process_and_blocks_resume(self):
        with patch.object(broker, "atomic_json", side_effect=OSError("ENOSPC")):
            with self.assertRaises(broker.EffectError) as caught:
                broker.operational("latch_kill", {"reason": "owner_request"}, "617744661")
        self.assertTrue(caught.exception.effect_attempted)
        broker.kill_attempted = False  # simulate broker restart
        self.assertTrue(broker.state()["kill_latched"])
        with self.assertRaisesRegex(RuntimeError, "kill_latched"):
            broker.operational("resume_entries", {}, "617744661")

    def test_health_preserves_canonical_reason_when_discovery_incomplete(self):
        broker.atomic_json(broker.ROSTER, {"prepared_count": 3})
        canonical = {
            "protocol": "trader20.control.v1", "operation": "runtime_health",
            "degraded": True, "reason": "canonical_risk_blocker", "data": {},
        }
        with patch.object(broker, "proxy_read", return_value=canonical):
            result = broker.handle({"operation": "runtime_health", "params": {}, "actor_id": "617744661"})
        self.assertEqual("canonical_risk_blocker;leader_discovery_incomplete", result["reason"])

    def test_resume_rollback_failure_is_ambiguous_effect(self):
        broker.operational("pause_entries", {"reason": "test"}, "617744661")
        with patch.object(broker, "atomic_hold", side_effect=OSError("hold")), \
             patch.object(broker, "save_state", side_effect=[None, OSError("rollback")]):
            with self.assertRaises(broker.EffectError) as caught:
                broker.operational("resume_entries", {}, "617744661")
        self.assertTrue(caught.exception.effect_attempted)
        self.assertIn("entry_hold_authoritative", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
