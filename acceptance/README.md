# cpe-labs Acceptance Suite

Wire-format regression gate for the simulator. Each scenario boots cpe-sim against a hermetic mock environment, captures the bytes on the wire, normalizes run-specific fields, and diffs against a committed golden file.

If a future PR drifts the wire shape — even when every unit test passes — the matching scenario goes red.

## Running

```bash
make acceptance         # run every scenario, fail on golden mismatch
make acceptance-update  # regenerate goldens (inspect the diff before committing)
```

Standard `go test ./...` and `make test` skip this directory entirely (build tag `acceptance` is required). The suite has no dependency on Docker or external services; everything runs in-process via embedded `mochi-mqtt` and `httptest`.

Wall-clock budget: each scenario is bounded under 30 seconds; the full suite should complete in under 2 minutes. CI runs `make acceptance` after the unit and race suites.

## Layout

```
acceptance/
├── README.md
├── doc.go                          # package decl + build tag
├── harness/
│   ├── harness.go                  # Fixture, StartCWMPAcceptance, LaunchSim, SimBinary, repoRoot
│   ├── broker.go                   # StartBroker (embedded mochi-mqtt)
│   ├── capture.go                  # WireCapture (CWMP + USP byte recorder)
│   ├── normalize.go                # NormalizeCWMP, NormalizeUSPRecord
│   ├── golden.go                   # CompareGolden, GoldenPath, -update flag
│   ├── golden_test.go              # self-tests for the golden helper
│   └── normalize_test.go           # self-tests for normalization rules
├── profiles/
│   └── minimal-tr181/              # the canonical acceptance profile
├── golden/
│   └── <scenario>/                 # one dir per scenario, contains the locked bytes
└── scenarios/
    └── <scenario>_test.go          # one file per scenario
```

## How a scenario works

Each scenario is a `Test*` function under `acceptance/scenarios/` carrying `//go:build acceptance`. The basic shape:

```go
func TestCWMP_FirstContact(t *testing.T) {
    fix := harness.StartCWMPAcceptance(t)
    harness.LaunchSim(t, fix, 30*time.Second, "--seed=1")

    snapshot := fix.Capture.Snapshot()
    body := harness.NormalizeCWMP(snapshot.CWMPRequests[0])
    harness.CompareGolden(t, "cwmp_first_contact/01_inform_request.xml", body)
}
```

The harness:

1. Builds the `cpe-sim` binary once per `go test` invocation (`SimBinary`, `sync.Once`).
2. Stands up the services the scenario needs (mock ACS, embedded MQTT broker, or both).
3. Hooks every wire-emitted byte into a `WireCapture`.
4. Runs `cpe-sim` as a subprocess with the scenario's args plus the harness's `--profile=` and `--acs-url=`.
5. Returns the captured bytes for the scenario to normalize and golden-compare.

## Determinism

cpe-sim is launched with `--seed=1` so per-CPE RNG (jitter, generators, fabricators) is reproducible. The `acceptance/profiles/minimal-tr181/` profile strips every non-deterministic surface beyond the base; richer scenarios may use richer profiles, but each profile must keep its output deterministic post-normalization.

What `NormalizeCWMP` replaces:

- `<cwmp:ID ...>nnn</cwmp:ID>` → `<cwmp:ID ...>{ID}</cwmp:ID>`
- `<CurrentTime>...</CurrentTime>` → `<CurrentTime>{TIMESTAMP}</CurrentTime>`
- `<ConnectionRequestURL>...</...>` → `<ConnectionRequestURL>{CR_URL}</...>`

What `NormalizeUSPRecord` replaces (when USP scenarios land):

- `Header.msg_id` → `{MSG_ID}`

Everything else is preserved verbatim. If a future change drifts those preserved fields, the matching golden goes red — exactly what we want.

## Adding a new scenario

1. Create `acceptance/scenarios/<name>_test.go` with `//go:build acceptance` at the top.
2. Pick a fixture helper:
   - `harness.StartCWMPAcceptance(t)` for CWMP-only flows
   - For USP flows: bring up `harness.StartBroker(t)`, write a profile that points `usp.broker.address` at the broker host:port, drive cpe-sim, subscribe to `usp/v1/controller` from the test and stuff payloads into the `WireCapture`.
3. Launch the simulator: `harness.LaunchSim(t, fix, timeout, "--seed=1", ...)`.
4. Capture bytes; normalize via the helper that matches the protocol.
5. `CompareGolden(t, "<scenario>/<step>.<ext>", normalized)`.
6. Run `make acceptance-update` once to write the initial golden.
7. **Read the generated golden carefully.** A golden you didn't inspect is a golden you don't trust.
8. Commit the test, the golden, and a `<scenario>/README.md` documenting what the golden locks down.

## Flakes

This suite should not flake. If a scenario flakes:

- The normalizer is missing a run-specific field. Add a rule to `NormalizeCWMP` or `NormalizeUSPRecord` and regenerate the golden.
- A wall-clock dependency leaked in. Tests should never sleep waiting for the simulator beyond the explicit timeout passed to `LaunchSim`.
- A generator or fabricator is firing. The minimal profile excludes them; if a scenario needs them, it must seed and bound them.

When in doubt, run the scenario 5× back-to-back. If any run differs, the cause is one of the above — fix the harness, not the test.

## Why a separate suite

Unit and integration tests assert on semantics; acceptance tests assert on **bytes**. Both useful, both should run, but the failure modes are different. A unit test failing means "the logic is wrong"; an acceptance test failing means "the wire shape changed, even if every unit test passes."

This is the gate that lets the project take outside contributors safely. A patch that fails the acceptance suite hasn't broken a unit test — it's broken the wire contract real ACSes depend on.
