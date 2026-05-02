# Periodic Inform Scheduler

This page covers the **TR-069 (CWMP)** periodic Inform timer. The TR-369 (USP) equivalent (periodic Notify driven by `Device.LocalAgent.Subscription.{i}`) is described in [USP Agent](usp.md).

Each simulated CPE has its own periodic Inform timer. The scheduler reads the interval and enable leaves from the parameter tree on every tick, so an SPV from the ACS reschedules immediately. Default jitter is ±10% uniform from a per-CPE RNG.

## Profile wiring

Two leaves drive the timer; the profile names them under `periodicInformPaths`:

```yaml
periodicInformPaths:
  interval: Device.ManagementServer.PeriodicInformInterval
  enable:   Device.ManagementServer.PeriodicInformEnable
```

Both leaves must exist in the parameter tree:

- `interval` is `xsd:unsignedInt`, writable, in seconds.
- `enable` is `xsd:boolean`, writable.

The loader rejects with a precise error if either is missing, the wrong type, or non-writable.

## Daemon mode is implicit

When `periodicInformPaths` is set, `bin/cpe-sim` runs as a daemon: it fires the bootstrap Inform, then leaves the process alive and lets the scheduler drive subsequent Informs.

When `periodicInformPaths` is omitted, the simulator exits after the bootstrap Inform unless `--cr-bind-addr` keeps it alive for ACS-initiated connection requests.

## Tick semantics

Each registered CPE runs in its own goroutine watching one `*time.Timer`. On tick:

1. The scheduler `TryLock`s the per-CPE `SessionMu`.
2. If the lock is held by another session (a CR session, a one-shot TransferComplete, a previous tick still in flight), the tick is **dropped** (logged at `debug`, not retried). A real CPE that misses a periodic timer because of an in-progress session simply waits for the next one.
3. Otherwise, the registered `OnTick` callback runs synchronously. `cmd/cpe-sim` wires that to `cwmp.RunSession(ctx, runOpts, TriggerPeriodic)`.
4. After `OnTick` returns, the scheduler re-reads `interval` from the tree (in case it changed) and arms the next tick.

Errors from `OnTick` are logged but do not stop the loop. Subsequent ticks proceed.

## SPV-driven reschedule

When the ACS issues an SPV against the interval or enable leaf, the SPV handler calls `scheduler.OnIntervalChange(cpeID)`. The scheduler:

1. Sets a non-blocking signal on the per-CPE change channel.
2. The drain goroutine wakes up, re-reads `interval` and `enable` from the tree, stops the current timer, and arms a fresh one.

The reschedule is **immediate**: there is no waiting for the current interval to elapse. An SPV that drops `interval` from 300 to 30 takes effect on the next tick scheduled from that moment.

When `enable` flips to `false`, the timer stops and the goroutine sleeps on the change channel until the next signal. The CPE keeps its bootstrap state and stays reachable via CR; it just stops emitting periodic Informs.

## Jitter

The next tick fires at `interval + delta` where `delta` is uniform in `[-jitter*interval, +jitter*interval]`. Default `jitter` is **0.10** (±10%). The random source is the per-CPE `*rand.Rand` derived from FNV-64a hash of `(rootSeed, cpeID)` (see [Multi-CPE Fleets](multi-cpe.md#determinism)).

Concretely, with `interval = 300s` and `jitter = 0.10`, ticks land somewhere in `[270s, 330s]` from the previous tick.

Pass `--seed=N` to reproduce a fleet's jitter pattern byte-for-byte. Without it, the scheduler picks a time-derived root seed and logs it as `root_seed=<N>` so a later run can replay.

## Minimum interval

Values below `1s` are clamped to `1s`. A real CPE will not emit Informs faster than that and a `0` value would thrash the goroutine.

## One-shot deliveries

The scheduler also services one-shot fires for `TransferComplete` (after a Download or Upload completes) and for the deferred Reboot / FactoryReset Informs configured via `eventSchedule:` (see [Profile YAML reference](../reference/profile-yaml.md#eventschedule)). Same `SessionMu` rules apply: the one-shot goroutine acquires the per-CPE mutex before invoking its callback, so it serializes against periodic ticks and CR sessions.

The handler returns a cancel function the caller can invoke to abort before fire. Cancel after fire is harmless. Repeat Reboot / FactoryReset RPCs supersede the in-flight one-shot via this cancel function.

## Concurrency model recap

- One `*Scheduler` instance per process services every CPE.
- One goroutine per CPE for the periodic loop.
- One additional goroutine per pending one-shot.
- Tree reads / writes serialize through the existing `paramtree.Tree` RWMutex.
- `SessionMu` (one per CPE) serializes periodic / CR / one-shot deliveries against each other.
