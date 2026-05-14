# cwmp_cr_roundtrip

What this scenario locks down: the wire shape of the follow-on Inform
a CPE fires in response to an ACS-initiated Connection-Request.

## Sequence

1. cpe-sim daemon-mode boots with `--cr-bind-addr=127.0.0.1:<port>`
   (test pre-picks a free port).
2. Bootstrap Inform fires (captured as `CWMPRequests[0]`, not goldened
   here — covered by `cwmp_first_contact`).
3. Test GETs the CR URL with digest auth (RFC 7616 qop=auth/MD5,
   matching cpe-sim's CR listener). Asserts 200.
4. cpe-sim opens a follow-on session → the Inform in this golden.

## What is in the golden

- SOAP/CWMP envelope (identical shape to bootstrap), `{ID}` sentinel.
- `<DeviceId>` block from the profile.
- `<Event>` block contains TWO `<EventStruct>`s:
  - `<EventCode>6 CONNECTION REQUEST</EventCode>` — the CR trigger
  - `<EventCode>2 PERIODIC</EventCode>` — the simulator's
    `EventTracker.NextSessionEvents` appends this when daemon mode
    queues an Inform under an unspecified trigger; captured here as
    the actual behavior even though `periodicInformPaths` is NOT
    declared in this profile. If a future simulator change drops
    `2 PERIODIC` from CR Informs (justified or not), this golden
    breaks and we investigate.
- `<RetryCount>0</RetryCount>` — fresh session.
- `<CurrentTime>` → `{TIMESTAMP}`.
- `<ParameterList>` carries only `Device.DeviceInfo.SoftwareVersion`
  per the profile's `informParameters.connectionRequest`.

## How the URL is discovered

cpe-sim runs with `--cr-bind-addr=127.0.0.1:0`; the kernel picks a
random port at `listener.Start()` time. cpe-sim then writes the
resolved URL into `Device.ManagementServer.ConnectionRequestURL`
(per `--cr-publish-path`). The bootstrap Inform's `ParameterList`
carries that leaf because the profile lists it under
`informParameters.bootstrap`. The test reads it from the captured
Inform body via `harness.ExtractCWMPParameter`.

Earlier versions of this scenario worked around a publish-before-Start
bug (the URL was empty in the tree); cpe-labs-context #20 fixed the
publish ordering and this scenario now discovers the URL via the
intended bootstrap-ParameterList path.

## How to update

```
make acceptance-update
```

Inspect the diff. A change to the event codes, the ParameterList, or
the envelope shape is a wire-format regression unless it tracks an
intentional simulator change.
