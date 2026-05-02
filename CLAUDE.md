# cpe-labs

A high-performance CPE simulator for **TR-069 (CWMP)** and **TR-369 (USP)**. One Go binary simulates many CPEs concurrently against an ACS or Controller, driven by operator-supplied vendor profiles in YAML.

> Workflow scaffolding (specs, ROADMAP, prompts, skills) lives in a separate repo: [`herder-labs/cpe-labs-context`](https://github.com/herder-labs/cpe-labs-context). This repo is code-only.

## Architecture Anchors (load-bearing — do not violate)

These constraints exist for a reason. Every PR must respect them; reviewers cite them by number.

1. **Simulate the management plane, not the operating system.** The simulator must be indistinguishable from a real CPE on the wire (TR-069 CWMP and TR-369 USP), not model Linux, kernel timers, NAT tables, or radio physics. If a feature is observable only via SSH or a serial console, it does not belong here. If it shows up in an Inform, a `GetParameterValues` response, a USP Notify, or a connection-request callback, it belongs here.

2. **Protocol-agnostic core, transports are adapters.** A simulated CPE has one in-memory parameter tree and one behavior engine. TR-069 (HTTP/SOAP) and TR-369 (USP over MQTT/WebSocket/STOMP/CoAP) are transport adapters that read from and write to that tree. Adding a new transport must not require touching the parameter tree, the behavior engine, or the vendor profile loader.

3. **Behavior is config, not code.** Operators describe a vendor's quirks and a device's runtime behavior in YAML/JSON: which parameters appear in the bootstrap Inform, what gets reported on each periodic Inform, which counters increment, how many WiFi/LAN clients to fabricate, how MAC OUIs are drawn. Go code evaluates generic rules over operator-supplied profiles. **No** `switch` on `"Sagemcom"` / `"Arris"` / `"Nokia"` in core code, **no** hardcoded TR-098 vs TR-181 path branches in the behavior engine, **no** embedded data models for specific devices.

4. **Vendor extensibility is the product.** Anyone introduces a new vendor, model, or firmware variant by dropping in a vendor profile (parameter tree + behavior rules + transport prefs) without recompiling. A design that forces a contributor to submit a Go PR to support their CPE is the wrong design.

5. **One process, many CPEs.** A single binary simulates thousands of CPEs concurrently with goroutines, not processes or containers. Per-CPE state lives in plain structs. Transport sessions multiplex over shared HTTP/MQTT/WebSocket clients where the protocol allows it. Per-CPE memory is measured and budgeted, not assumed.

6. **Determinism is opt-in, randomness is the default.** Real fleets are noisy: jittered Inform intervals, fluctuating signal strength, clients joining and leaving, MAC churn. The behavior engine produces this drift by default. Every random source accepts a seed (per-CPE or global) so an operator reproduces a scenario exactly when they need to debug an ACS or write a regression test.

7. **Standards-faithful before vendor-quirky.** Out of the box, a simulated CPE is BBF-compliant (TR-069 / TR-369 / TR-181 / TR-098). Vendor quirks (`X_*` extensions, malformed XML, non-standard fault codes) layer on top via the vendor profile, never baked into the core encoder/decoder.

## Repository Layout

```
cmd/cpe-sim/              # Binary entrypoint
internal/
  paramtree/              # In-memory parameter tree + profile loader (the spine)
  cwmp/                   # TR-069 stack: soap, inform, transport, scheduler, cr, handlers, transfer
  generators/             # Counter / drift / enum / uptime / wallclock value generators
  cperng/                 # Per-CPE seeded RNG
  cpeconfig/              # CLI / env / YAML config loader
  cpelog/, cpeerr/        # Structured logging + typed errors
  testgolden/             # Per-package golden-test helper
  version/                # Build-time version metadata
profiles/                 # YAML vendor profiles (built-in examples)
docs/                     # MkDocs site
```

## Doing the work

- `make test`, `make test-race`, `make build`, `make lint`, `make fmt`, `make vet`.
- Conventional Commits: `feat(scope): ...`, `fix(scope): ...`, etc.
- Branch naming: `<type>/<scope>-<description>`, e.g. `feat/usp-mqtt-mtp`.
- One feature per PR.
- Issues + spec workflow live in [`cpe-labs-context`](https://github.com/herder-labs/cpe-labs-context). Open new feature issues over there if you want them tracked through the spec process; trivial fixes can go straight to a PR here.

## Writing style (applies to code, comments, commit messages, PR bodies, docs)

- **No em dashes.** Use parentheses, commas, periods, semicolons, or rephrase.
- **No "Related" / "See also" / "Further reading" sections at the bottom of docs.** Cross-link inline where the topic comes up.
- **Don't reference proprietary or unrelated projects by name.** This is a generic, vendor-neutral simulator; the docs read that way.
- Code comments default to none. Add a comment only when the WHY is non-obvious.

## Documentation

User-facing docs render at the project's MkDocs site. Any change that touches the vendor profile schema, CLI flags / env vars, transports, or behavior primitives must update the matching docs page in the same PR.
