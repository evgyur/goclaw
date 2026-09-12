#!/usr/bin/env python3
"""Narrow host broker between Haraldr GoClaw and the incumbent Trader20 v3 owner.

The broker has no exchange SDK, signer, wallet secret, or order endpoint. Reads are
proxied to the canonical Trader20 control projection. Operational controls only
set the independent entry-hold override after authoritative local readbacks.
Manual plan/execute remains fail-closed until a candidate-bound operator lane is
provisioned in the incumbent writer.
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import socket
import socketserver
import tempfile
import time

SOCKET = Path(os.environ.get("TRADER20_HARALDR_SOCKET", "/run/trader20-haraldr-control/control.sock"))
READ_SOCKET = Path(os.environ.get("TRADER20_READ_SOCKET", "/run/trader20-control-read/control.sock"))
STATE_ROOT = Path(os.environ.get("TRADER20_STATE_ROOT", "/var/lib/trader20-v3/state"))
CURRENT = Path(os.environ.get("TRADER20_CURRENT", "/opt/trader20-v3/current"))
HOLD_ENV = Path(os.environ.get("TRADER20_HARALDR_HOLD_ENV", "/etc/trader20-v3/haraldr-control.env"))
CONTROL_STATE = STATE_ROOT / "haraldr-control" / "state.json"
ACTIVATION = STATE_ROOT / "activation" / "trader20-v3-active.json"
WS = STATE_ROOT / "ws-shadow" / "signals_raw.json"
ROSTER = STATE_ROOT / "leader-rotation" / "leader_roster_latest.json"
WATCH_STATE = STATE_ROOT / "operator-receipts" / "copy-readiness-watch-state.json"
MAX_REQUEST = 64 * 1024
MAX_RESPONSE = 4 * 1024 * 1024
READ_OPS = {"capabilities", "status", "positions", "orders", "history", "explain_blocker", "runtime_health"}
CONTROL_OPS = {"pause_entries", "resume_entries", "latch_kill", "cancel_pending_plan", "plan_trade", "execute_plan"}


def now_ms() -> int:
    return int(time.time() * 1000)


def load_json(path: Path) -> dict:
    try:
        value = json.loads(path.read_text())
    except (OSError, ValueError, TypeError):
        return {}
    return value if isinstance(value, dict) else {}


def atomic_json(path: Path, value: dict, mode: int = 0o600) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream, sort_keys=True, allow_nan=False)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(tmp, mode)
        os.replace(tmp, path)
        dfd = os.open(path.parent, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(dfd)
        finally:
            os.close(dfd)
    finally:
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass


def atomic_hold(held: bool) -> None:
    HOLD_ENV.parent.mkdir(parents=True, exist_ok=True)
    payload = f"TRADER20_V3_ENTRY_HOLD={'1' if held else '0'}\n"
    fd, tmp = tempfile.mkstemp(prefix=HOLD_ENV.name + ".", dir=HOLD_ENV.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            stream.write(payload)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(tmp, 0o600)
        os.replace(tmp, HOLD_ENV)
        dfd = os.open(HOLD_ENV.parent, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(dfd)
        finally:
            os.close(dfd)
    finally:
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass


def proxy_read(operation: str, params: dict) -> dict:
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
        client.settimeout(60)
        client.connect(str(READ_SOCKET))
        client.sendall(json.dumps({"operation": operation, "params": params}, allow_nan=False).encode() + b"\n")
        with client.makefile("rb") as stream:
            raw = stream.readline(MAX_RESPONSE + 1)
    if len(raw) > MAX_RESPONSE or not raw.endswith(b"\n"):
        raise RuntimeError("canonical_read_response_invalid")
    value = json.loads(raw)
    if not isinstance(value, dict):
        raise RuntimeError("canonical_read_response_invalid")
    return value


def service_active(unit: str) -> bool:
    # systemd state is read from /run/systemd rather than invoking a shell. The
    # broker intentionally has no generic command execution surface.
    try:
        import subprocess
        result = subprocess.run(
            ["/usr/bin/systemctl", "is-active", unit], check=False,
            capture_output=True, text=True, timeout=3,
        )
        return result.stdout.strip() == "active"
    except Exception:
        return False


def readiness() -> tuple[bool, list[str], dict]:
    reasons: list[str] = []
    activation = load_json(ACTIVATION)
    current = str(CURRENT.resolve()) if CURRENT.exists() else ""
    candidate = str(activation.get("candidate_sha") or activation.get("candidate") or "")
    status = str(activation.get("status") or "")
    release = str(activation.get("release") or activation.get("release_path") or "")
    if status != "ACTIVE":
        reasons.append("activation_not_active")
    if not current or (release and current != release):
        reasons.append("current_release_mismatch")
    ws = load_json(WS)
    heartbeat = ws.get("producer_heartbeat_ms", ws.get("heartbeat_ms"))
    complete = ws.get("complete_leader_count", ws.get("leader_count"))
    halted = ws.get("entries_halted")
    if not isinstance(heartbeat, int) or now_ms() - heartbeat > 120_000 or heartbeat > now_ms() + 5_000:
        reasons.append("websocket_stale")
    if complete != 6 or halted is not False:
        reasons.append("websocket_not_exact_six")
    if not service_active("trader20-v3.timer"):
        reasons.append("writer_timer_inactive")
    if not service_active("trader20-v3-ws-ingestor.service"):
        reasons.append("websocket_ingestor_inactive")
    return not reasons, reasons, {
        "activation_status": status,
        "candidate_sha": candidate,
        "current_release": current,
        "activation_release": release,
        "websocket_complete_leaders": complete,
        "websocket_entries_halted": halted,
        "websocket_heartbeat_ms": heartbeat,
        "writer_timer_active": service_active("trader20-v3.timer"),
        "websocket_ingestor_active": service_active("trader20-v3-ws-ingestor.service"),
    }


def state() -> dict:
    value = load_json(CONTROL_STATE)
    return {
        "schema": "trader20.haraldr-control-state.v1",
        "kill_latched": value.get("kill_latched") is True,
        "kill_reason": value.get("kill_reason"),
        "entries_paused": value.get("entries_paused") is True,
        "pause_reason": value.get("pause_reason"),
        "updated_at_ms": value.get("updated_at_ms"),
    }


def save_state(value: dict) -> None:
    value = dict(value)
    value["schema"] = "trader20.haraldr-control-state.v1"
    value["updated_at_ms"] = now_ms()
    atomic_json(CONTROL_STATE, value)


def authorized_actor(actor: object) -> str:
    expected = os.environ.get("TRADER20_OPERATOR_USER_ID", "").strip()
    actual = str(actor or "").split("|", 1)[0].strip()
    if not expected or actual != expected:
        raise PermissionError("operator_not_authorized")
    return actual


def operational(operation: str, params: dict, actor: object) -> dict:
    principal = authorized_actor(actor)
    current_state = state()
    reason = str(params.get("reason") or "operator_request").strip()
    if not reason or len(reason) > 160:
        raise ValueError("reason_invalid")
    if operation == "pause_entries":
        atomic_hold(True)
        current_state.update(entries_paused=True, pause_reason=reason)
        save_state(current_state)
        return {"ok": True, "state": "ENTRIES_PAUSED", "effect": "entry_hold_enabled", "actor": principal}
    if operation == "latch_kill":
        atomic_hold(True)
        current_state.update(kill_latched=True, kill_reason=reason, entries_paused=True, pause_reason="kill_latched")
        save_state(current_state)
        return {"ok": True, "state": "KILL_LATCHED", "effect": "entry_hold_enabled", "actor": principal}
    if operation == "resume_entries":
        if current_state["kill_latched"]:
            raise RuntimeError("kill_latched_requires_out_of_band_recovery")
        ready, reasons, evidence = readiness()
        if not ready:
            raise RuntimeError("resume_fail_closed:" + ",".join(reasons))
        atomic_hold(False)
        current_state.update(entries_paused=False, pause_reason=None)
        save_state(current_state)
        return {"ok": True, "state": "ENTRIES_ENABLED", "effect": "entry_hold_released", "actor": principal, "evidence": evidence}
    if operation == "cancel_pending_plan":
        raise RuntimeError("no_operator_plan_lane_provisioned")
    if operation in {"plan_trade", "execute_plan"}:
        raise RuntimeError("operator_money_lane_requires_new_candidate_bound_authority")
    raise ValueError("unsupported_operation")


def make_envelope(operation: str, data: object, *, degraded: bool = False, reason: str = "") -> dict:
    activation = load_json(ACTIVATION)
    return {
        "protocol": "trader20.control.v1",
        "operation": operation,
        "captured_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "stale": False,
        "degraded": degraded,
        "reason": reason,
        "candidate_sha": activation.get("candidate_sha") or activation.get("candidate"),
        "data": data,
    }


def handle(value: dict) -> dict:
    if not isinstance(value, dict) or set(value) - {"operation", "params", "actor_id"}:
        raise ValueError("request_invalid")
    operation = value.get("operation")
    params = value.get("params", {})
    if not isinstance(params, dict):
        raise ValueError("params_invalid")
    operational_ops = {"pause_entries", "resume_entries", "latch_kill", "cancel_pending_plan"}
    money_ops = {"plan_trade", "execute_plan"}
    if operation == "capabilities":
        return make_envelope(operation, {
            "operations": sorted(READ_OPS | CONTROL_OPS),
            "operational_controls": sorted(operational_ops),
            "money_controls": sorted(money_ops),
            "money_control_available": False,
            "money_control_blocker": "candidate_bound_authority_and_writer_adapter_not_provisioned",
            "single_writer": "trader20-v3",
            "raw_exchange_credentials": False,
            "direct_exchange_write": False,
        })
    if operation in READ_OPS:
        result = proxy_read(operation, params)
        if operation == "runtime_health" and isinstance(result, dict):
            ready, reasons, evidence = readiness()
            control = state()
            roster = load_json(ROSTER)
            active_count = roster.get("active_count", roster.get("activeCount"))
            prepared_count = roster.get("prepared_count", roster.get("preparedCount"))
            data = result.get("data")
            if not isinstance(data, dict):
                data = {"canonical_projection": data}
            data["haraldr_management"] = {
                "money_lane_ready": ready and not control["entries_paused"] and not control["kill_latched"],
                "readiness_reasons": reasons,
                "runtime": evidence,
                "control": control,
                "leader_active_count": active_count,
                "leader_prepared_count": prepared_count,
                "leader_discovery_degraded": isinstance(prepared_count, int) and prepared_count < 20,
                "legacy_copy_watch_retired": True,
            }
            result["data"] = data
            result["degraded"] = bool(result.get("degraded")) or not ready or (isinstance(prepared_count, int) and prepared_count < 20)
            if not ready:
                result["reason"] = ",".join(reasons)
            elif isinstance(prepared_count, int) and prepared_count < 20:
                result["reason"] = "leader_discovery_incomplete"
        return result
    if operation in CONTROL_OPS:
        data = operational(operation, params, value.get("actor_id"))
        return make_envelope(operation, data)
    raise ValueError("unsupported_operation")


class Handler(socketserver.StreamRequestHandler):
    def handle(self) -> None:
        self.request.settimeout(65)
        try:
            raw = self.rfile.readline(MAX_REQUEST + 1)
            if len(raw) > MAX_REQUEST or not raw.endswith(b"\n"):
                raise ValueError("request_too_large")
            result = handle(json.loads(raw))
        except Exception as exc:
            result = {
                "protocol": "trader20.control.v1",
                "operation": "denied",
                "captured_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "stale": False,
                "degraded": True,
                "reason": str(exc)[:240],
                "data": None,
                "effect_attempted": False,
            }
        encoded = json.dumps(result, sort_keys=True, allow_nan=False).encode() + b"\n"
        if len(encoded) > MAX_RESPONSE:
            encoded = b'{"protocol":"trader20.control.v1","operation":"denied","degraded":true,"reason":"response_too_large","effect_attempted":false}\n'
        self.wfile.write(encoded)


def main() -> int:
    SOCKET.parent.mkdir(parents=True, exist_ok=True)
    if SOCKET.exists():
        raise SystemExit("control_socket_already_exists")
    with socketserver.UnixStreamServer(str(SOCKET), Handler) as server:
        os.chmod(SOCKET, 0o660)
        server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
