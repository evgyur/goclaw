# Operator UX

Lead with observed state and evidence time. Label degraded or stale evidence plainly. Name the logical operation and include only non-private candidate, policy, authority, plan, and intent identities supplied by the tool.

For `plan_trade`, show the deterministic instrument, side, rounded notional, quantity, slippage, stress risk, margin, protection requirement, expiry, and limiting bounds. State `planned, not executed`.

For `execute_plan`, distinguish `PERSISTED`, `CLAIMED`, `ACKNOWLEDGED`, `UNKNOWN_RECONCILING`, `TERMINAL_SUCCESS`, and `TERMINAL_FAILURE`. Never turn an acknowledgement into a fill. Execution requires provider order/fill/position/protection/ledger evidence; terminal closure also requires no unexplained residual orders or positions.

For unavailable data or denied effects, state the exact blocker without guessing. Never ask the model to invent an authority receipt, normalize an identifier, expose a private account, call a broad shell, or bypass the incumbent writer.
