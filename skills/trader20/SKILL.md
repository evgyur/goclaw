---
name: trader20
description: "Use for Trader20 reads and bounded control requests."
version: 2.0.1
metadata:
  protocol: trader20.control.v1
  effect_scope: brokered_bounded_control
---

# Trader20

Use the platform adapter in `adapters/` for exactly: `capabilities`, `status`, `positions`, `orders`, `history`, `explain_blocker`, `runtime_health`, `plan_trade`, and `execute_plan`.

## Required workflow

1. Call `capabilities` and the relevant fresh read operation. Never infer live state from memory or prose.
2. Preserve asset, token, candidate, tree, policy, account-binding, authority, plan, intent, nonce, and protocol identifiers exactly. Never normalize or repair an identifier.
3. Cite evidence time. If `stale=true`, `degraded=true`, an identity binding is absent, or the tool fails, describe the result as unavailable/degraded rather than live.
4. Keep account identifiers private. Do not expose or conflate wallet, Hyperliquid master, Privy, Pear, leader, or agent-wallet identities.
5. Report only tool-envelope fields. Never invent an execution, order, fill, protection, authority, signature, or delivery receipt.

## Bounded operator-request workflow

`plan_trade` and `execute_plan` are separate brokered operations, not direct exchange tools.

1. Use only lane `OPERATOR_REQUEST`; never convert model prose, a plan approval, or a natural signal into effect authority.
2. Require a fresh authenticated `PLAN` receipt before `plan_trade`. It must bind the exact request, candidate commit/tree, account fingerprint, policy hash, principal, client, origin, owner-sender hash, authority envelope, nonce, expiry, maximum intent count, maximum gross notional, and 5 bps execution cap.
3. Present the deterministic plan without claiming execution.
4. Require a different fresh authenticated `EXECUTE` receipt bound to the exact plan before `execute_plan`.
5. Treat receipt/authority expiry, replay, hash drift, limit exhaustion, KILL/pause, stale market/source, pending effects, unavailable protection, writer drift, and provider ambiguity as fail-closed blockers.
6. A persisted or acknowledged intent is not a trade. Claim execution only from provider order/fill/position/protection/ledger evidence with no unexplained residual effects.

## Hard boundary

This skill has no raw exchange credential, signer, wallet, shell, skill-publication, transfer, withdrawal, or direct exchange-write tool. Money effects may leave only through the incumbent Trader20 writer after the control service validates exact authority and risk gates. Never call broad shell or raw provider APIs as a substitute.

All Hyperliquid and HIP-3 instruments remain visible exactly as canonical discovery returns them. See `references/control-protocol.md`, `references/risk-and-authority.md`, and `references/operator-ux.md`.

## Output contract

Return operation, evidence time, freshness/degraded state, exact non-private candidate/policy/authority/plan/intent bindings when supplied, state transition, and blocker. Separate planned, persisted, acknowledged, provider-executed, and terminally reconciled states.
