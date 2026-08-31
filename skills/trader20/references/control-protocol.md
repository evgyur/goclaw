# trader20.control.v1 protocol

Allowed logical operations, in canonical order:

1. `capabilities`
2. `status`
3. `positions`
4. `orders`
5. `history`
6. `explain_blocker`
7. `runtime_health`
8. `plan_trade`
9. `execute_plan`

The first seven operations are observation-only. Their normalized envelope is `contracts/trader20-control-v1/read-envelope.schema.json`.

`plan_trade` accepts only the exact schema in `contracts/trader20-control-v1/plan-trade-request.schema.json`. `execute_plan` accepts only `execute-plan-request.schema.json`. `OPERATOR_REQUEST` requires a separately typed receipt at both stages; `NATURAL_SIGNAL` must carry null. The authority envelope, runtime snapshot, risk policy, and receipt schemas are packaged with both platform projections from the same source commit and core hash.

The client never signs or calls an exchange write endpoint. Effectful requests go only to the Trader20 control service, which persists and fsyncs before acknowledgement and hands a claimed intent to the incumbent writer. Missing control transport or authority fails closed.

A response is live only when identity bindings are present and freshness/degraded fields permit that interpretation. Provider ambiguity is `UNKNOWN_RECONCILING`; it never authorizes blind retry.
