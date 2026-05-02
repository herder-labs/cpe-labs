# cpe-labs

A high-performance CPE simulator written in Go. One binary simulates many CPEs concurrently over **TR-069 (CWMP)** and **TR-369 (USP)**, driven by operator-supplied vendor profiles in YAML.

The simulator is faithful to the BBF standards on the wire. It does not model the underlying operating system: if it shows up in an Inform, a `GetParameterValues` response, a USP Notify, or a connection-request callback, it belongs here.

## Quickstart

```bash
make build
bin/cpe-sim \
    --profile=profiles/example-tr181-gateway/ \
    --acs-url=http://your-acs.local:7547/
```

That sends one bootstrap Inform and exits when the ACS closes the session.

For daemon mode (periodic Informs + connection-request listener), multi-CPE fleets, generators, Docker, compose, CI integration, USP MTPs (MQTT / WebSocket / STOMP), and the full vendor-profile reference, see the [docs site](https://herder-labs.github.io/cpe-labs/).

## What it does

- TR-069 SOAP envelopes, RPC dispatch, full session lifecycle. Eleven core methods (GPV, GPN, GPA, SPV, SPA, AddObject, DeleteObject, Reboot, FactoryReset, Download, Upload, plus TransferComplete).
- TR-369 USP record framing over MQTT, WebSocket, and STOMP.
- TR-181 (`Device.*`) and TR-098 (`InternetGatewayDevice.*`) parameter trees.
- Periodic Informs on a tree-driven timer with per-CPE jitter.
- ACS-initiated Connection Requests with HTTP Basic / Digest auth and per-CPE throttling.
- Five value generators (counter, drift, enum, uptime, wallclock) for moving telemetry between Informs.
- Multi-CPE fleets per process with named CIDR pools for IPv4 / IPv6 / IPv6 delegated prefix.
- Scheduled Reboot / FactoryReset / boot Inform emission for realistic wall-clock latency.
- Deterministic via `--seed=N` (or `CPE_SIM_SEED=N`).

## Docker

```bash
docker pull herderlabs/cpe-sim:latest
docker run --rm herderlabs/cpe-sim --profile=/profiles/example-tr181-gateway/ --acs-url=http://acs:7547/
```

The image bundles the reference profiles at `/profiles/`.

## Project layout

```
cmd/cpe-sim/        Binary entrypoint
internal/           Go packages: paramtree, cwmp, generators, scheduler, cperng, etc.
profiles/           Built-in vendor profile examples
docs/               MkDocs source for the docs site
```

## Configuration

`cpe-sim` reads configuration from four sources, in deterministic precedence (highest first):

1. CLI flags (`--acs-url`, `--log-level`, etc.)
2. Environment variables (prefix `CPE_SIM_`, e.g. `CPE_SIM_ACS_URL`)
3. Optional YAML config file (`--config /path/to/config.yaml` or `CPE_SIM_CONFIG=/path/...`)
4. Compiled defaults

Unknown YAML keys, unknown `CPE_SIM_*` env vars, and unknown flags all return errors. Run `bin/cpe-sim --help` for the full flag list, or see the [CLI reference](https://herder-labs.github.io/cpe-labs/reference/cli/).

## Development

**Prerequisites:** Go 1.25+ and `golangci-lint`.

```bash
make test       # go test ./...
make test-race  # go test -race ./...
make build      # go build ./...
make lint       # golangci-lint run
make fmt        # gofmt
make vet        # go vet ./...
```

For local docs preview: `pip install -r requirements.txt && mkdocs serve`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Specs and the broader workflow live in a separate repository: [`herder-labs/cpe-labs-context`](https://github.com/herder-labs/cpe-labs-context).

## License

[Apache License 2.0](LICENSE).
