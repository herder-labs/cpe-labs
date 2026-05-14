# usp_operate_reboot

What this scenario locks down: the two-step wire flow when a
controller drives `Operate(Device.Reboot())` against the agent.

## Sequence

1. Bootstrap fires OnBoardRequest (drained as setup; covered by
   `usp_first_contact`).
2. Controller publishes `Msg{Operate("Device.Reboot()")}` to
   `usp/v1/agent/<eid>`.
3. Agent responds synchronously with `OperateResp` on
   `usp/v1/controller/reply-to=<encoded-agent-topic>`.
4. After `eventSchedule.rebootDelay` (1s in the acceptance profile),
   the agent emits `Notify(Event{Boot!})` matching the default-seed
   Subscription row on `Device.Boot!`.

## 01_operate_resp.txt

Locked-down fields:

- Record envelope: `version="1.5"`, `from_id` agent EID,
  `to_id="self::herder"`.
- Msg header: `msg_type=OPERATE_RESP`, `msg_id={MSG_ID}` (the
  agent echoes the controller's request msg_id; normalized).
- Body: `operate_resp.operation_results.executed_command =
  "Device.Reboot()"`, `req_output_args` empty (Reboot returns no
  args; the empty map is the wire shape).

## 02_boot_event.txt

Locked-down fields:

- Same Record envelope as 01.
- Msg header: `msg_type=NOTIFY`, fresh `msg_id` (normalized).
- Body: `notify.subscription_id="default-boot-event-ACS"` (the
  obuspa-fixture seed), `notify.event.obj_path="Device."`,
  `notify.event.event_name="Boot!"`.

## Pre-existing bug fixed alongside this scenario

`internal/usp/mtp/mqtt/client.go::onConnect` previously subscribed
to BOTH the exact agent inbox (`usp/v1/agent/<eid>`) AND the
multi-level wildcard (`usp/v1/agent/<eid>/#`). MQTT 3.1.1 §4.7.1.2
says the multi-level wildcard "represents the parent and any number
of child levels", so the wildcard alone matches the bare topic.
Subscribing to both made brokers like mochi-mqtt deliver the same
controller request twice — the agent handled every Operate
twice and emitted duplicate OperateResp publishes. The acceptance
suite catches this; the fix (drop the redundant exact subscription)
ships in the same PR.

## How to update

```
make acceptance-update
```

Inspect the diff. A change to the OperateResp shape, the Event
envelope, or the subscription_id is a wire-format regression unless
it tracks an intentional simulator change.
