# cpe-labs

<p align="center">
  <a href="https://github.com/herder-labs/cpe-labs/releases"><img src="https://img.shields.io/github/v/release/herder-labs/cpe-labs?include_prereleases&style=for-the-badge" alt="GitHub release"></a>
  <a href="https://hub.docker.com/r/herderlabs/cpe-sim"><img src="https://img.shields.io/docker/v/herderlabs/cpe-sim?label=Docker&logo=docker&logoColor=white&color=2496ED&style=for-the-badge&sort=semver" alt="Docker image"></a>
  <a href="https://github.com/herder-labs/cpe-labs/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache--2.0-blue.svg?style=for-the-badge" alt="Apache-2.0 License"></a>
  <a href="https://cpe-labs.herder-labs.io/"><img src="https://img.shields.io/badge/Docs-cpe--labs.herder--labs.io-green?style=for-the-badge" alt="Documentation"></a>
</p>

> **Documentation: https://cpe-labs.herder-labs.io/**

A high-performance CPE simulator for **TR-069 (CWMP)** and **TR-369 (USP)**. One container simulates many CPEs concurrently, driven by operator-supplied vendor profiles in YAML.

The simulator is faithful to the BBF standards on the wire. It does not model the underlying operating system: if it shows up in an Inform, a `GetParameterValues` response, a USP Notify, or a connection-request callback, it belongs here.

## Quickstart

```bash
docker run --rm herderlabs/cpe-sim \
    --profile=/profiles/example-tr181-gateway/ \
    --acs-url=http://your-acs.local:7547/
```

That sends one bootstrap Inform and exits when the ACS closes the session. The image bundles the reference profiles at `/profiles/`.

For daemon mode (periodic Informs + connection-request listener), multi-CPE fleets, generators, compose, CI integration, USP MTPs (MQTT / WebSocket / STOMP), and the full vendor-profile reference, see the [documentation](https://cpe-labs.herder-labs.io/).

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

## Configuration

`cpe-sim` reads configuration from four sources, in deterministic precedence (highest first):

1. CLI flags (`--acs-url`, `--log-level`, etc.)
2. Environment variables (prefix `CPE_SIM_`, e.g. `CPE_SIM_ACS_URL`)
3. Optional YAML config file (`--config /path/to/config.yaml` or `CPE_SIM_CONFIG=/path/...`)
4. Compiled defaults

Unknown YAML keys, unknown `CPE_SIM_*` env vars, and unknown flags all return errors. Run `docker run --rm herderlabs/cpe-sim --help` for the full flag list, or see the [CLI reference](https://cpe-labs.herder-labs.io/reference/cli/).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache License 2.0](LICENSE).
