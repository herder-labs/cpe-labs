# usp_set_value_change

What this scenario locks down: the autonomous Notify(ValueChange)
flow — the wire shape a controller-driven Set produces when a
matching Subscription is installed on the watched leaf.

## Sequence

1. Bootstrap fires OnBoardRequest (drained as setup; covered by
   `usp_first_contact`).
2. Controller publishes `Add` to install a Subscription:
   - `Subscription.Enable = "true"`
   - `Subscription.ID = "vc-sub-1"`
   - `Subscription.NotifType = "ValueChange"`
   - `Subscription.ReferenceList = "Device.WiFi.Radio.1.Channel"`
   - `Subscription.Recipient = "self::herder"`
3. Agent responds with AddResp (drained; covered by Bundle 1 unit
   tests). Subscription evaluator's 50ms debounced rescan picks
   up the new row.
4. Controller publishes `Set` against the watched leaf:
   `Device.WiFi.Radio.1.Channel = "11"`.
5. Agent responds with SetResp (golden 01) AND autonomously emits
   Notify(ValueChange) (golden 02). Both arrive within ~100ms of
   each other; the scenario classifies them by `msg_type` rather
   than capture order.

## 01_set_resp.txt

Locked-down fields:

- Record envelope (version, from_id, to_id, no_session_context
  cleared per normalizer).
- Msg header: `msg_type=SET_RESP`, `{MSG_ID}`.
- Body: `set_resp.updated_obj_results.requested_path =
  "Device.WiFi.Radio.1."`, `oper_success.updated_inst_results
  .affected_path` same, and `updated_params{Channel: "11"}`.

## 02_value_change_notify.txt

Locked-down fields:

- Same Record envelope.
- Msg header: `msg_type=NOTIFY`, `{MSG_ID}`.
- Body: `notify.subscription_id = "vc-sub-1"` (the controller's
  chosen ID, threaded through the evaluator's ValueChange path),
  `notify.value_change.param_path =
  "Device.WiFi.Radio.1.Channel"`, `param_value = "11"`.

## How to update

```
make acceptance-update
```

Inspect the diff. A change to the SetResp's `updated_inst_results`
shape, the Notify body's `subscription_id` or `value_change`
fields, or the Record envelope is a wire-format regression
unless it tracks an intentional simulator change.

## Why this scenario matters

The autonomous Notify(ValueChange) path is what #12 hardened (period
re-arm, OnWrite-once gate, armPeriodic stopped-flag, etc.). This
acceptance test locks down the byte-level emission shape so any
future change to the evaluator's `send` path breaks the golden
even when every unit test passes.
