# USP Agent (TR-369)

cpe-labs speaks **TR-369 (USP)** alongside TR-069. Same simulator, same vendor profile, same parameter tree; the USP Agent is just a second transport adapter over the in-memory tree.

This page covers the USP-side surface: how the Agent connects to a Controller, which MTPs are supported, and how Subscribe / Notify drives outbound traffic.

!!! info "v0 ships a foundation, not the full surface"
    Today the USP role is configured via a top-level `usp:` block in the **profile YAML**, not via CLI flags. MQTT 3.1.1 is the only MTP that ships; WebSocket and STOMP are forward-looking. Subscribe / Notify reconciliation, request handlers (Get / Set / Add / Delete / Operate / GetInstances / GetSupportedDM), and broader Notify variants are tracked in follow-up Phase 3 epics. Sections below that show `--usp-*` CLI flags or describe Subscription-driven notification describe the **forward design**; jump to the [v0 quickstart](#v0-quickstart) for the surface that actually ships today.

## v0 quickstart

The minimum profile to register a single simulated CPE with a USP controller is one `usp:` block on top of the existing `Device.DeviceInfo.*` and `deviceIdPaths` declarations:

```yaml
deviceIdPaths:
  manufacturer: Device.DeviceInfo.Manufacturer
  oui:          Device.DeviceInfo.ManufacturerOUI
  productClass: Device.DeviceInfo.ProductClass
  serialNumber: Device.DeviceInfo.SerialNumber

parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "ACME"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "GenericGateway"
  - path: Device.DeviceInfo.SerialNumber
    value: "GATEWAY-0001"

usp:
  enable: true
  broker:
    address: nats
    port: 1883
    protocolVersion: "3.1.1"
```

Run:

```bash
bin/cpe-sim --profile=profile.yaml --acs-url=http://your-acs.local:7547/
```

cpe-sim still requires `--acs-url` (CWMP is always wired today); on startup the simulator connects to the configured MQTT broker, subscribes to `usp/v1/agent/os::AABBCCGATEWAY-0001`, and publishes a `Notify(OnBoardRequest)` to `usp/v1/controller`. A USP-aware controller (for example the herder-labs USP controller on its dev stack) materializes a device row from that notification.

### First-contact gate

USP-enabled profiles install a system leaf `Internal.Reboot.Cause` with initial value `LocalFactoryReset`. The session reads it before emitting first contact:

| `Internal.Reboot.Cause` | First-contact emission |
| --- | --- |
| `LocalFactoryReset` | `Notify(OnBoardRequest)` with `oui` / `product_class` / `serial_number` / `agent_supported_protocol_versions = "1.5"`. Leaf flips to `LocalReboot` after emission. |
| `LocalReboot` (or anything else) | `Notify(Event{event_name: "Boot!", obj_path: "Device."})` |

Process restart resets the leaf back to `LocalFactoryReset` because `LoadProfile` rebuilds the tree from scratch (v0 process-restart-as-factory-reset semantics; cross-restart NVRAM persistence is deferred).

### Running against a herder-labs / OpenACS dev stack

If the controller's dev stack is running locally with the NATS-MQTT bridge enabled, cpe-labs joins the broker network and registers as the device-side simulator that obuspa would otherwise play in the reference fixture:

```bash
bin/cpe-sim \
  --profile=/path/to/profile-with-usp.yaml \
  --acs-url=http://localhost:7547/   # any CWMP endpoint works; bootstrap fires first
```

Container deployments must join the same Docker network the broker exposes; the broker host is the container service name (e.g. `nats`) rather than `127.0.0.1`.

## Forward-looking architecture

The sections below describe the longer-term USP architecture cpe-labs is building toward, not what `bin/cpe-sim` exposes today. Specifically:

- CLI flags like `--usp-mtp=*`, `--usp-mqtt-addr=*`, `--usp-ws-url=*`, `--usp-stomp-addr=*` are **not yet implemented**. All USP knobs go through the profile's `usp:` block.
- WebSocket and STOMP MTPs are not yet implemented.
- Request handlers `Get`, `Set`, `Add`, `Delete`, `Operate(Device.Reboot())`, `GetInstances`, `GetSupportedDM` **are implemented**. Async-Request `Operate` and `Operate(Device.FactoryReset())` remain.
- Autonomous `Notify(ObjectCreation)`, `Notify(ObjectDeletion)`, `Notify(ValueChange)`, `Notify(Event)`, `Notify(Periodic)` **are implemented** and gated by the per-CPE Subscription evaluator. Operators provision Subscriptions via the existing `Set` / `Add` / `Delete` handlers against `Device.LocalAgent.Subscription.{i}.`.
- `Notify(OperationComplete)` remains; deferred until the controller exercises async-Request `Operate`.

## Request handlers (Bundle 1)

The agent dispatches inbound USP messages by `Header.msg_type`. Five handlers ship today:

| Message | Behavior |
| --- | --- |
| `Get` | Returns `GetResp` with one `RequestedPathResult` per requested path (input order preserved). Per-path `err_code: 7026` on unknown paths does not fail the rest. v0 supports concrete scalar paths only; wildcards and object-walk are documented follow-ups. |
| `Set` | Atomic via `Tree.SetBatch`. `allow_partial: false` rolls back on any failure with per-param `ParameterError`. `allow_partial: true` returns `Error: 7008` in v0 (the reference controller hardcodes `false`). Wires the shared `valueChange` callback so generators / SPV consumers see the same write events. |
| `Add` | Allocates a new instance, applies `param_settings`, populates `unique_keys` from the profile's `objects[].uniqueKeys` declarations. SetBatch failure rolls back the just-created instance. Emits an autonomous `Notify(ObjectCreation)` with matching `unique_keys`. |
| `Delete` | Deletes the instance, returns `affected_paths`. Missing instance returns `err_code: 7404` (Object does not exist). Paths without trailing `.` return `7026`. Emits an autonomous `Notify(ObjectDeletion)` with the deleted prefix. |
| `Operate(Device.Reboot())` | Synchronous `OperateResp` (req_output_args empty); flips `Internal.Reboot.Cause` to `LocalReboot` and schedules `Event{Boot!}` Notify after `eventSchedule.rebootDelay` (default 0 = immediate). Repeat reboots supersede the in-flight schedule. Unknown commands return `cmd_failure: 7022`. |

Unmapped message types (`GetInstances`, `GetSupportedDM`, etc.) return an `Error` Msg with `err_code: 7000`, msg_id echoed. The agent dispatch table never silently drops a request.

## Multi-instance objects: `uniqueKeys`

For Add to return useful `unique_keys` (and ObjectCreation Notifies to be meaningful to the controller), the profile must declare each multi-instance object's unique-key sets:

```yaml
objects:
  - path: Device.WiFi.SSID
    instances: 1
    uniqueKeys:
      - [SSID]            # one set, single param
      - [BSSID]           # another set, single param
    parameters:
      - path: SSID
        ...
```

Each entry under `uniqueKeys` is a key-set (a list of parameter names relative to the object). cpe-labs reads the matching leaves under the newly-instantiated instance to populate the AddResp / ObjectCreation `unique_keys` map. Omitting the block produces an empty `unique_keys` map; the controller falls back to identifying instances by `instantiated_path` alone. See [profile-yaml.md](../reference/profile-yaml.md#usp) for the full schema.

## Subscription-driven Notify

When `usp.enable: true`, cpe-labs auto-installs `Device.LocalAgent.Subscription.{i}.` as a writable multi-instance table and seeds instance 1 with the obuspa-fixture-equivalent Boot! event Subscription (`Enable: true`, `ID: default-boot-event-ACS`, `NotifType: Event`, `ReferenceList: Device.Boot!`, `Persistent: true`). The seed row is mutable; operators may override it, disable it, or add new rows via the existing `Set` / `Add` / `Delete` handlers.

Each enabled Subscription drives a per-NotifType trigger:

| NotifType | Trigger | Notify emitted |
| --- | --- | --- |
| `Event` | Matching event fires (e.g. `Device.Boot!` after `Operate(Device.Reboot())`) | `Notify(Event)` with `event_name` |
| `ValueChange` | A leaf in `ReferenceList` changes value (via `Set`, generator write, etc.) | `Notify(ValueChange)` with `param_path` + `param_value` |
| `ObjectCreation` | A multi-instance row materializes under a prefix in `ReferenceList` (via `Add`) | `Notify(ObjectCreation)` with `obj_path` + `unique_keys` |
| `ObjectDeletion` | A multi-instance row is removed under a prefix in `ReferenceList` (via `Delete`) | `Notify(ObjectDeletion)` |
| `Periodic` | `Period`-second cadence (±10% jitter) | `Notify(ValueChange)` per path in `ReferenceList` |

Subscription matching is exact-path for `Event` / `ValueChange` and prefix for `ObjectCreation` / `ObjectDeletion`. The evaluator debounces table-write rescans by 50ms so a controller doing multi-row provisioning sees its writes settle before triggers register.

`OperationComplete` Subscriptions and cross-restart Subscription persistence are out of scope for now.

## Architectural fit

The USP Agent reads from and writes to **the same `paramtree.Tree`** that the CWMP stack uses. Per-CPE state is shared:

- One parameter tree per simulated CPE / Agent instance.
- One profile (the YAML schema is transport-agnostic).
- One generator runner (counters / drift / enum / uptime / wallclock leaves move regardless of which protocol reads them).
- One scheduler entry per CPE (USP `Subscription` periodic notify reuses the same timer plumbing as periodic Inform).

cpe-labs runs in **one process** with many simulated Agents in goroutines (see [Architecture](../overview/architecture.md) anchor #5). One outbound MTP client (one MQTT broker connection, one WebSocket session, one STOMP session) is shared across the fleet where the protocol allows it; per-Agent USP records multiplex over that shared client.

## Picking an MTP

USP rides one of three MTPs in cpe-labs:

| MTP | When to use | cpe-labs flag |
| --- | --- | --- |
| **MQTT** | Most common in production deployments. Broker-mediated, NAT-friendly. | `--usp-mtp=mqtt` |
| **WebSocket** | Direct Controller↔Agent over a single TCP connection. | `--usp-mtp=websocket` |
| **STOMP** | Legacy / message-broker shops with existing STOMP infra. | `--usp-mtp=stomp` |

Each MTP has its own connection knobs but the USP record framing on top is identical. Switching MTPs does not require profile changes.

## MQTT

```bash
bin/cpe-sim \
  --profile=profile.yaml \
  --usp-mtp=mqtt \
  --usp-mqtt-addr=broker.example.com:1883 \
  --usp-mqtt-user=cpe-user \
  --usp-mqtt-password=cpe-pass
```

| Flag | Env | Default | Notes |
| --- | --- | --- | --- |
| `--usp-mqtt-addr` | `CPE_SIM_USP_MQTT_ADDR` | (required when `--usp-mtp=mqtt`) | `host:port`. |
| `--usp-mqtt-user` | `CPE_SIM_USP_MQTT_USER` | "" | Optional broker auth. |
| `--usp-mqtt-password` | `CPE_SIM_USP_MQTT_PASSWORD` | "" | |
| `--usp-mqtt-tls` | `CPE_SIM_USP_MQTT_TLS` | `false` | Wrap the connection in TLS. |
| `--usp-mqtt-ca-cert-file` | `CPE_SIM_USP_MQTT_CA_CERT_FILE` | "" | PEM bundle for broker cert verification. |

USP MQTT topic shapes are not fixed by the spec; the Agent and Controller agree on a topic prefix per deployment via `Device.LocalAgent.MTP.{i}.MQTT.SubscribeTopic` and `ResponseTopicConfigured`. Configure those leaves in the profile to match what your Controller expects.

The endpoint ID is derived from the per-CPE serial: `os::<oui>-<productClass>-<serial>`. For example, with `(OUI=AABBCC, ProductClass=GenericGateway, Serial=GATEWAY-0001)` the Agent registers as `os::AABBCC-GenericGateway-GATEWAY-0001`.

## WebSocket

```bash
bin/cpe-sim \
  --profile=profile.yaml \
  --usp-mtp=websocket \
  --usp-ws-url=ws://controller.example.com:8080/ws/agent
```

| Flag | Env | Default | Notes |
| --- | --- | --- | --- |
| `--usp-ws-url` | `CPE_SIM_USP_WS_URL` | (required when `--usp-mtp=websocket`) | Full `ws://` or `wss://` URL. |
| `--usp-ws-tls-skip-verify` | `CPE_SIM_USP_WS_TLS_SKIP_VERIFY` | `false` | Insecure; for self-signed test Controllers. |
| `--usp-ws-ca-cert-file` | `CPE_SIM_USP_WS_CA_CERT_FILE` | "" | PEM bundle for `wss://` cert verification. |

Each Agent opens its own WebSocket session. The shared transport pool reuses TCP-level resources where the protocol allows.

## STOMP

```bash
bin/cpe-sim \
  --profile=profile.yaml \
  --usp-mtp=stomp \
  --usp-stomp-addr=stomp.example.com:61613 \
  --usp-stomp-user=cpe-user \
  --usp-stomp-password=cpe-pass
```

Same auth and TLS flag pattern as MQTT.

## Endpoint identity

USP Agents identify themselves with an Endpoint ID. cpe-labs builds it deterministically from the parameter tree:

```
os::<DeviceInfo.ManufacturerOUI>-<DeviceInfo.ProductClass>-<DeviceInfo.SerialNumber>
```

Per-CPE differentiation works the same as CWMP: `fleet.serialPattern` and the `{cpe:*}` placeholders stamp unique serials, and the Endpoint ID follows automatically.

## Subscribe / Notify

The USP equivalent of TR-069's periodic Inform is `Device.LocalAgent.Subscription.{i}.` driven outbound notifications. A Controller creates a Subscription with `NotifType=Periodic` and a `Periodic.Period`; the Agent fires Notify records on that interval.

cpe-labs reuses the same scheduler the CWMP stack uses (see [Periodic Inform Scheduler](scheduler.md)). Per-CPE jitter is the same ±10% default, derived from the same per-CPE RNG. `--seed=N` reproduces both stacks byte-for-byte.

Other `NotifType` values:

- **`ValueChange`**: fires when a leaf the subscription references mutates. Generators that move tree state (counter, drift, enum) trigger this.
- **`ObjectCreation`** / **`ObjectDeletion`**: fires when an Agent-side AddObject / DeleteObject runs.
- **`OperationComplete`**: fires when a long-running Operate (USP equivalent of CWMP's TransferComplete) finishes.

All Subscription state lives in the parameter tree under `Device.LocalAgent.Subscription.{i}.`. The profile loads it the same way it loads any other multi-instance object.

## No Connection Request

USP has no equivalent of TR-069 §3.2.2 Connection Request. The Agent maintains a persistent connection to the Controller via the chosen MTP, and the Controller sends Get / Set / Operate / Add / Delete records over that session whenever it needs to. There is no "wake up the Agent" round-trip.

This means **`--cr-bind-addr` is a no-op for USP-only deployments**. In dual-stack deployments (the Agent speaks both CWMP and USP), the CR listener serves the CWMP side only.

## Same profile, both stacks

Nothing in the vendor profile is CWMP-specific. The same `profiles/example-tr181-gateway/` works against an ACS over CWMP or a Controller over USP. Toggle between them with the relevant `--acs-url` / `--usp-mtp` flags.

Dual-stack mode (Agent speaks both protocols simultaneously) is supported by combining the flag sets:

```bash
bin/cpe-sim \
  --profile=profile.yaml \
  --acs-url=http://acs.example.com:7547/cwmp \
  --usp-mtp=mqtt \
  --usp-mqtt-addr=broker.example.com:1883
```

Reads from the ACS and reads from the Controller see the same tree. Writes from either side mutate the same tree. Generators move state regardless of who's looking.

## Determinism across protocols

USP record framing is deterministic given a seed: the per-CPE RNG drives Subscription jitter, generator jitter, and any vendor-quirky reordering hooks the profile enables. Pass `--seed=N` and an entire dual-stack run replays byte-for-byte.

## What's next

- Smoke-test against a Controller you own.
- Build a profile that mirrors a real device's `Device.LocalAgent.MTP.{i}.` and `Device.LocalAgent.Controller.{i}.` shape so the Controller sees the topology it expects.
- Use the same fleet placeholders documented in [Multi-CPE Fleets](multi-cpe.md) to differentiate Endpoint IDs, MTP credentials, and per-Agent topic subscriptions.
