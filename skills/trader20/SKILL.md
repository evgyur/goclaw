---
name: trader20
description: "Use when reading or explaining Trader20 status, positions, orders, history, capabilities, blockers, or runtime health. Uses only trader20.control.v1 read operations and fails closed on stale or unbound evidence."
version: 1.0.0
metadata:
  protocol: trader20.control.v1
  effect_scope: read_only
---

# Trader20

Use the platform adapter in `adapters/` to map these logical operations: `capabilities`, `status`, `positions`, `orders`, `history`, `explain_blocker`, and `runtime_health`.

## Required workflow

1. Call the matching read operation; do not infer live state from memory or prose.
2. Preserve asset, token, candidate, policy, and protocol identifiers exactly. Never normalize or repair an identifier.
3. Cite `captured_at` and `source_timestamp` when present. If `stale=true`, `degraded=true`, an identity binding is absent, or the tool fails, describe the result as unavailable/degraded rather than live.
4. Keep account identifiers private. Do not expose or conflate wallet, Hyperliquid master, Privy, Pear, leader, or agent-wallet identities.
5. Report only fields in the tool envelope. Never invent an execution, order, fill, protection, signer, or delivery receipt.

## Hard boundary

This skill is strictly read-only. It cannot sign, plan, place, modify, cancel, or close trades; manage wallets; transfer or withdraw funds; or claim that any such effect occurred. Reject direct effect requests and explain that Release A exposes no execution operation. The presence of an open-order read operation does not create order-write authority.

All Hyperliquid and HIP-3 instruments remain visible exactly as returned by canonical provider discovery; never silently narrow support. See `references/control-protocol.md`, `references/risk-and-authority.md`, and `references/operator-ux.md`.

## Output contract

Return a concise answer containing operation, evidence time, freshness/degraded state, candidate/policy identities when available, source-backed data, and any blocker reason. Unsupported or effectful claims must be explicitly rejected.
