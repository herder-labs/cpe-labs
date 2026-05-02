# USP Agent (TR-369)

cpe-labs speaks **TR-369 (USP)** alongside TR-069. Same simulator, same vendor profile, same parameter tree; the USP Agent is just a second transport adapter over the in-memory tree.

This page covers the USP-side surface: how the Agent connects to a Controller, which MTPs are supported, and how Subscribe / Notify drives outbound traffic.

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
