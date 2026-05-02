# Contributing to cpe-labs

Thanks for considering a contribution.

## How to contribute

### Bug fixes and trivial changes

Open a PR directly. Include the bug report or motivation in the description. No spec required.

### New features or behavior changes

File an issue first so the design can be discussed.

### Documentation

User-facing docs live in `docs/` and render via [MkDocs](https://www.mkdocs.org/). Any change that touches the vendor profile schema, CLI flags / env vars, transports, or behavior primitives must update the matching docs page in the same PR.

## Development setup

**Prerequisites:** Go 1.25+ and `golangci-lint`.

```bash
git clone https://github.com/herder-labs/cpe-labs.git
cd cpe-labs
make build
make test
make lint
```

For docs:

```bash
pip install -r requirements.txt
mkdocs serve
# Open http://127.0.0.1:8000/
```

## Coding standards

### Architecture anchors

Seven load-bearing constraints. Reviewers cite them by number; violating them breaks the simulator's extensibility model and its scale promise:

1. **Simulate the management plane, not the OS.** If a feature is observable only via SSH or a serial console, it does not belong here.
2. **Protocol-agnostic core, transports are adapters.** TR-069 and TR-369 read from / write to one in-memory parameter tree. Adding a new transport must not require touching the tree, the behavior engine, or the profile loader.
3. **Behavior is config, not code.** Operators describe vendor quirks and runtime behavior in YAML. No `switch` on vendor names, no hardcoded TR-181 vs TR-098 branches in core, no embedded data models.
4. **Vendor extensibility is the product.** A new vendor / model / firmware lands as a profile, not a Go PR.
5. **One process, many CPEs.** Per-CPE state lives in plain structs. Transport sessions multiplex over shared clients. Anything that scales linearly in goroutines or file descriptors per CPE needs justification.
6. **Determinism is opt-in, randomness is the default.** Real fleets are noisy; behavior reflects that. `--seed` reproduces any scenario byte-for-byte when needed.
7. **Standards-faithful before vendor-quirky.** Out of the box, BBF-compliant. Vendor quirks (`X_*` extensions, malformed XML, non-standard fault codes) layer on via the profile, not the core encoder/decoder.

The most common rejection reasons:

- **Hardcoded vendor / model knowledge in core Go code.** Anything that switches on `"Sagemcom"` / `"Arris"` / a specific OUI, or branches on TR-181 vs TR-098 path prefixes, belongs in the operator-supplied profile, not in `internal/`.
- **Per-CPE OS resources.** No per-CPE TCP ports, processes, or files. `internal/cwmp/cr` shows the pattern: one socket, path-routed by CPE ID.
- **Code that requires a vendor PR to introduce a new device.** If your design forces every contributor to ship Go to add their CPE, the design is wrong.

### Style

- **Conventional Commits**: `<type>(<scope>): <description>`. Types: `feat`, `fix`, `docs`, `refactor`, `test`, `ci`, `chore`, `build`.
- **Branches**: `<type>/<scope>-<description>`, e.g. `feat/usp-mqtt-mtp`.
- **One feature per PR.** Don't bundle.
- **No em dashes** in any text (commits, comments, PR bodies, docs). Use parentheses, commas, periods, semicolons, or rephrase.
- **Don't reference unrelated projects by name** in docs or comments. The simulator is generic and vendor-neutral.
- **Default to no comments.** Only add a comment when the WHY is non-obvious.
- **Test budget**: per-package tests should run in under 30s. Use real time for short-duration integration tests (200ms is the convention; see `cmd/cpe-sim/main_test.go`).

## Tests

Every behavior change ships with tests. The project uses a per-package `testdata/golden/` convention with the `internal/testgolden` helper:

```go
testgolden.Compare(t, "case_name.xml", got)
```

Run tests for a single package: `go test ./internal/cwmp/handlers/...`. Run with the race detector: `make test-race`.

Update goldens after a deliberate output change: `go test ./<pkg>/... -update`.

## Pull request checklist

- [ ] Tests cover the change, including failure paths.
- [ ] `make lint` and `make test` green.
- [ ] `gofmt -l .` clean.
- [ ] Docs updated if user-facing surface changed.
- [ ] PR body explains the **why**, not just the what.
- [ ] Conventional commit message.

## License

By contributing, you agree your contribution is licensed under the [Apache License 2.0](LICENSE).
