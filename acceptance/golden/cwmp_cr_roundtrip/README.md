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

## Why pre-pick the CR port

The scenario passes a known `127.0.0.1:<port>` to cpe-sim rather than
letting cpe-sim bind to `:0` and reading the URL out of the bootstrap
Inform's ParameterList. cpe-sim publishes the URL to the tree at
`registerCREndpoint` time, BEFORE `listener.Start()` actually binds
the port — see `cmd/cpe-sim/main.go:1244` +
`internal/cwmp/cr/listener.go::URL`. The published value is therefore
empty regardless of bind-addr. Pre-picking is the simpler workaround
until the publish-after-start ordering is fixed (a tiny runtime bug
worth filing separately).

## How to update

```
make acceptance-update
```

Inspect the diff. A change to the event codes, the ParameterList, or
the envelope shape is a wire-format regression unless it tracks an
intentional simulator change.
