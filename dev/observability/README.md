# Standalone observability stack

Local Prometheus + Grafana for validating cpe-sim's `--metrics-bind-addr`. Independent of any other compose project.

## Bring it up

```bash
docker compose -f dev/observability/docker-compose.yaml up -d
```

- Prometheus: <http://127.0.0.1:9091>
- Grafana:    <http://127.0.0.1:3031>  (anonymous Admin, no login)
- Dashboard:  Grafana → Dashboards → "cpe-sim"

## Run cpe-sim against an ACS

The stack expects cpe-sim listening on the host at `:9100`. The Prometheus container reaches it via `host.docker.internal` (Linux: configured with `--add-host`).

```bash
# TR-069 example against any ACS
go run ./cmd/cpe-sim \
  --profile=./profiles/example-tr181-gateway \
  --acs-url=http://localhost:7547/cwmp \
  --metrics-bind-addr=0.0.0.0:9100 \
  --log-level=info
```

For TR-369 / USP, point the profile's `usp.broker.address` at whatever broker the controller uses (e.g. `127.0.0.1` if you're running NATS-MQTT locally) and start cpe-sim the same way.

## Tear down

```bash
docker compose -f dev/observability/docker-compose.yaml down
```

Add `-v` to also drop the Prometheus + Grafana volumes if you want a clean slate.
