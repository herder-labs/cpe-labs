# usp_first_contact

What this scenario locks down: the two wire shapes a fresh cpe-sim
emits on first MQTT publish, depending on `Internal.Reboot.Cause`.

## 01_onboard_request.txt

Factory-fresh: `Internal.Reboot.Cause` defaulting to `LocalFactoryReset`.
The session loop reads the leaf, calls `notify.OnBoardRequestBuilder`,
and publishes `Notify(OnBoardRequest)`.

Locked-down fields:

- Record envelope: `version="1.5"`, `from_id=os::AABBCCACCEPTANCE-0001`,
  `to_id=self::herder`, `no_session_context` present (payload cleared
  by the normalizer because it carries the raw Msg bytes which
  include a non-deterministic `msg_id`).
- Msg header: `msg_type=NOTIFY`, `msg_id` replaced with `{MSG_ID}`.
- Notify body: `on_board_req` with `oui`, `product_class`,
  `serial_number`, `agent_supported_protocol_versions`.

## 02_boot_event.txt

Warm-boot: profile pre-declares `Internal.Reboot.Cause=LocalReboot`,
so the session loop calls `notify.BootEventBuilder` instead and
publishes `Notify(Event{Boot!})`.

Locked-down fields:

- Same Record envelope as above.
- Notify body: `subscription_id=default-boot-event-ACS` (the
  obuspa-fixture seed value), `event.obj_path=Device.`,
  `event.event_name=Boot!`.

## What is normalized

`acceptance/harness/normalize.go::NormalizeUSPRecord` substitutes:

- `Header.msg_id` → `{MSG_ID}` (per-send random)
- `Record.no_session_context.payload` → cleared (contains the
  raw Msg bytes, which include the non-deterministic `msg_id`
  encoded in protobuf wire format; the decoded Msg is rendered
  separately below)

Everything else is preserved verbatim. A future change to the
OnBoardRequest field set, the Event payload shape, or the Record
envelope breaks the matching golden.

## How to update

```
make acceptance-update
```

Inspect the diff. Anything other than added/removed protobuf fields
or controller-EID changes is worth scrutiny.
