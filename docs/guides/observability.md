# Observability

cpe-sim exposes Prometheus metrics and a small admin introspection surface on a single HTTP listener controlled by `--metrics-bind-addr` (env `CPE_SIM_METRICS_BIND_ADDR`, YAML `metricsBindAddr`). Both are off by default; setting the bind address enables them and keeps the process running in daemon mode even if no other long-lived feature is configured.

```bash
cpe-sim \
  --profile ./profiles/example-tr181-gateway \
  --acs-url http://acs.local:7547/cwmp \
  --metrics-bind-addr 127.0.0.1:9100
```

The v0 server has no authentication. Bind it to localhost in dev and only expose it externally inside a trust boundary.

## Endpoints

| Method | Path | Returns |
| --- | --- | --- |
| `GET` | `/metrics` | Prometheus scrape format. |
| `GET` | `/admin/cpes` | JSON list of every CPE: id, serial, lifecycle_state, last_event, last_event_at. |
| `GET` | `/admin/cpes/{id}` | JSON detail: above plus 32-entry recent-event tail, tree-leaf sample, USP Subscription summary. |
| `POST` | `/admin/cpes/{id}/cr` | Triggers a synthetic Connection-Request session for the CPE. Returns 202 with `{cr_url, invoked_at}`. |
| `POST` | `/admin/cpes/{id}/fault/{code}` | Queues a CWMP fault `code` for the next session. Returns 202. |

`force CR` and `inject fault` return `503 Service Unavailable` when the underlying hook is not yet wired (Phase 7 lands the metrics surface; the CR / fault hooks bind to existing internals in follow-up issues).

## Metrics catalog

All metrics are namespaced `cpe_sim_*`. Per-CPE identity is intentionally **not** a label. Aggregating across the fleet is the design choice that lets a single time-series scale to thousands of CPEs without Prometheus cardinality issues; use the admin endpoint when you need per-CPE introspection.

### Process

- `cpe_sim_process_heap_alloc_bytes` (gauge): Go heap currently allocated.
- `cpe_sim_process_goroutines` (gauge): live goroutine count.
- `cpe_sim_process_fd_count{os}` (gauge): open file descriptors. `os="linux"` reads `/proc/self/fd`; other platforms emit 0 with `os="unsupported"`.
- `cpe_sim_process_uptime_seconds_total` (counter): wall-clock uptime since startup.
- `cpe_sim_process_gc_pause_seconds` (histogram): per-cycle GC pause.

### Fleet

- `cpe_sim_fleet_cpes_by_state{lifecycle_state}` (gauge): count of CPEs in each lifecycle state. States: `new`, `bootstrapping`, `ready`, `failed`. Polled every 5s.
- `cpe_sim_fleet_bootstraps_total` (counter): successful first-contact bootstrap sessions.
- `cpe_sim_fleet_failed_sessions_total` (counter): bootstrap or periodic sessions that returned an error.
- `cpe_sim_fleet_periodic_ticks_total` (counter): periodic-Inform timer ticks fired.
- `cpe_sim_fleet_scheduled_events_fired_total` (counter): deferred Reboot / FactoryReset / boot events fired.

### CWMP

- `cpe_sim_cwmp_informs_total{event_code, result}` (counter): Inform sessions, broken out per event code and result (`ok` / `error`).

### USP

- `cpe_sim_usp_requests_total{msg_type, result}` (counter): USP requests handled.
- `cpe_sim_usp_request_rtt_seconds{msg_type}` (histogram): handler RTT (decode through send).
- `cpe_sim_usp_autonomous_notifies_total{notify_kind, gated}` (counter): autonomous Notifies emitted by the Subscription evaluator. `gated="true"` is the post-Bundle-2 path (every emission gated on a matching Subscription).
- `cpe_sim_usp_mqtt_connection_state{broker, state}` (gauge): MQTT broker connection state. States: `connected`, `connecting`, `disconnected`. Cardinality stays at `O(brokers)` regardless of fleet size.

### RPC faults

- `cpe_sim_rpc_faults_total{protocol, code}` (counter): protocol fault responses. `protocol="cwmp"` today; USP faults add `protocol="usp"` in a later iteration.

## Scrape config

```yaml
scrape_configs:
  - job_name: cpe-sim
    static_configs:
      - targets: ['127.0.0.1:9100']
    scrape_interval: 15s
```

## Alert recipes

These five alerts cover the failure modes the simulator's been instrumented for. Tune thresholds to your fleet size.

1. **Failed-bootstrap rate spike.** A sudden jump in `cpe_sim_fleet_failed_sessions_total` is almost always an ACS misconfig, network change, or credentials drift.

   ```promql
   rate(cpe_sim_fleet_failed_sessions_total[5m]) > 1
   ```

2. **Autonomous-notify backlog.** If `cpe_sim_usp_autonomous_notifies_total` flatlines while `cpe_sim_fleet_cpes_by_state{lifecycle_state="ready"}` stays high, the Subscription evaluator may be stuck.

   ```promql
   rate(cpe_sim_usp_autonomous_notifies_total[5m]) == 0
     and cpe_sim_fleet_cpes_by_state{lifecycle_state="ready"} > 0
   ```

3. **MQTT disconnected fleet.** Any CPEs in `state="disconnected"` for more than a minute indicates a broker or auth issue.

   ```promql
   sum by (broker) (cpe_sim_usp_mqtt_connection_state{state="disconnected"}) > 0
   ```

4. **Fault-rate spike.** A jump in `cpe_sim_rpc_faults_total` typically means the ACS sent malformed RPCs or the data model drifted.

   ```promql
   rate(cpe_sim_rpc_faults_total[5m]) > 0.1
   ```

5. **Heap growth without CPE growth.** If `cpe_sim_process_heap_alloc_bytes` climbs but `cpe_sim_fleet_cpes_by_state` is steady, you have a leak; bisect against the bench harness below.

   ```promql
   deriv(cpe_sim_process_heap_alloc_bytes[10m]) > 0
     and deriv(sum(cpe_sim_fleet_cpes_by_state)[10m]) == 0
   ```

## Benchmark harness

The per-CPE cost claim is verified by a `go test -bench` harness in `cmd/cpe-sim`:

```bash
go test -bench=. -run=^$ -benchtime=1x ./cmd/cpe-sim/
```

Output:

```
BenchmarkBootstrapN_100-16    1   32731652 ns/op   9632 bytes_per_cpe   0.06 goroutines_per_cpe   12 fds_open
BenchmarkBootstrapN_1000-16   1  139353111 ns/op   3533 bytes_per_cpe   0.006 goroutines_per_cpe   12 fds_open
```

Read it as:

- `ns/op`: total wall time for one iteration of the named scale (N CPEs bootstrapped).
- `bytes_per_cpe`: heap delta over baseline, divided by N. Approximate; GC can drop allocations between samples.
- `goroutines_per_cpe`: goroutines retained after the iteration, divided by N.
- `fds_open`: total file descriptors open at the end of the iteration (process-level, not per-CPE).

Paste the most-recent output into the PR description for any change that touches per-CPE state. A future PR may add a CI check that fails when `bytes_per_cpe` regresses past a threshold.

The USP RTT benchmark (`BenchmarkUSPRequestResponseRTTN_*`) is gated behind `USP_BENCH=1` because it spins up the embedded MQTT broker; it reports `rtt_p50_us` / `rtt_p95_us` / `rtt_p99_us` when run.
