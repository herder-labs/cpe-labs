# cwmp_first_contact

What this scenario locks down: the first Inform a fresh cpe-sim CPE
posts to an ACS it has never spoken to before.

## What is in the golden

- SOAP/CWMP envelope with the standard four namespaces (`soapenv`,
  `cwmp`, `xsi`, `xsd`).
- `<cwmp:ID>` — replaced with `{ID}` (per-process atomic counter,
  non-deterministic across runs).
- `<DeviceId>` block carrying the four `Device.DeviceInfo.*` leaves
  the profile declares.
- `<Event>` block: both `1 BOOT` and `0 BOOTSTRAP` because the
  simulator emits both events on first contact (BOOT for any startup;
  BOOTSTRAP because no prior session has flagged the bootstrap
  bit).
- `<MaxEnvelopes>1</MaxEnvelopes>` — the simulator's standard value.
- `<CurrentTime>` — replaced with `{TIMESTAMP}` (wall clock).
- `<RetryCount>0</RetryCount>` — preserved verbatim; deterministic.
- `<ParameterList>` — every leaf listed under the profile's
  `informParameters.bootstrap` array, in declaration order.

## What is normalized

`acceptance/harness/normalize.go` substitutes the three run-specific
fields:

- `<cwmp:ID ...>nnn</cwmp:ID>` → `<cwmp:ID ...>{ID}</cwmp:ID>`
- `<CurrentTime>...</CurrentTime>` → `<CurrentTime>{TIMESTAMP}</CurrentTime>`
- `<ConnectionRequestURL>...` → `<ConnectionRequestURL>{CR_URL}</...>`
  (the CR URL appears only in scenarios that enable the CR listener;
  this scenario does not).

## How to update

```
make acceptance-update
```

then inspect the diff before committing. A diff that changes the
DeviceId, EventCode, MaxEnvelopes, or ParameterList shape is a
genuine wire-format change — review carefully.
