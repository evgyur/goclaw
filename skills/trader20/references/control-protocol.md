# trader20.control.v1 read protocol

The canonical normalized envelope is defined by `contracts/trader20-control-v1/read-envelope.schema.json`.

Allowed logical operations are exactly:

- `capabilities`
- `status`
- `positions`
- `orders`
- `history`
- `explain_blocker`
- `runtime_health`

Every operation is observation-only. The transport may issue only approved Hyperliquid `/info` requests. There is no execute, sign, trade, place, amend, cancel, close, transfer, withdrawal, wallet, or builder operation in Release A.

A response is live only when the tool succeeds, identity bindings are present where required, and both `stale` and `degraded` are false. `captured_at` is observation time; `source_timestamp` is source evidence time when available. Missing source time must not be invented.
