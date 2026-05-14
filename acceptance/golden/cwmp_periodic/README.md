# cwmp_periodic

What this scenario locks down: the wire shape of the **periodic**
Inform — the second Inform session a daemon-mode cpe-sim fires at
the configured `periodicInformPaths.interval` cadence after the
bootstrap session completes.

## What is in the golden

- Same SOAP/CWMP envelope as bootstrap.
- `<cwmp:ID>` — `{ID}` sentinel.
- `<DeviceId>` block — identical to bootstrap.
- `<Event>` block contains exactly one `<EventStruct>` with
  `<EventCode>2 PERIODIC</EventCode>` (not BOOTSTRAP / BOOT).
- `<MaxEnvelopes>1</MaxEnvelopes>`.
- `<CurrentTime>` — `{TIMESTAMP}` sentinel.
- `<RetryCount>0</RetryCount>`.
- `<ParameterList>` carries only `Device.DeviceInfo.SoftwareVersion`
  and `Device.DeviceInfo.HardwareVersion` (the profile's
  `informParameters.periodic` block; deliberately trimmed for
  signal-to-noise vs the bootstrap list).

## Setup

- Profile: `acceptance/profiles/minimal-tr181-periodic/` (extends
  minimal-tr181 with periodicInformPaths and a 1-second interval,
  the scheduler's enforced minimum).
- `--seed=1` makes jitter deterministic.
- Test budget: 5 seconds wall-clock (bootstrap ~50ms + 1s + 10%
  jitter + capture overhead).

## How to update

```
make acceptance-update
```

Inspect the diff. A change to the event code, the ParameterList
contents, or the envelope shape is a wire-format regression unless
it tracks an intentional simulator change.
