# Profile YAML Reference

A profile is YAML (or JSON) describing one CPE model: parameter tree, periodic Inform paths, fleet metadata, generators, connection-request auth, and transfer behavior. The same profile drives both the [TR-069 (CWMP)](../guides/acs-integration.md) and [TR-369 (USP)](../guides/usp.md) stacks; nothing in the schema is transport-specific.

A profile is **either a single file** (e.g. `profile.yaml`) **or a directory** of `*.yaml` / `*.yml` files that load in lexicographic order and merge into one tree. Use a directory when one file is getting unwieldy; group leaves by topic (`deviceinfo.yaml`, `wifi.yaml`, `hosts.yaml`).

This page is the exhaustive field reference. For introductions and worked examples, see:

- [Profile Schema](../guides/profiles.md): overview and minimum viable profile.
- [Multi-CPE Fleets](../guides/multi-cpe.md): `fleet:`, pools, placeholders.
- [Value Generators](../guides/generators.md): counter / drift / enum / uptime / wallclock.

## Top-level blocks

```yaml
deviceIdPaths:        # required
parameters:           # leaves
objects:              # multi-instance objects (table-shaped)
groups:               # single-instance prefix groupings
informParameters:     # per-event-code parameter lists for Inform builder
periodicInformPaths:  # leaves the per-CPE periodic Inform timer reads
generators:           # top-level generators list
fleet:                # fleet count + pools + serial pattern
connectionRequest:    # CR listener auth + throttle
transfer:             # Download / Upload TransferComplete defaults + faults
eventSchedule:        # Wall-clock latency for Reboot / FactoryReset / boot
```

Every block is optional except `deviceIdPaths` and at least one of `parameters` / `objects` / `groups` (you need to mount the four DeviceID leaves).

## `deviceIdPaths` (required)

```yaml
deviceIdPaths:
  manufacturer: Device.DeviceInfo.Manufacturer
  oui:          Device.DeviceInfo.ManufacturerOUI
  productClass: Device.DeviceInfo.ProductClass
  serialNumber: Device.DeviceInfo.SerialNumber
```

Names the four leaves the Inform builder reads to populate the `<DeviceId>` block. All four are required when the block is present; partial declarations reject. The simulator reads these paths from the tree at every Inform.

For TR-098 substitute the `InternetGatewayDevice.DeviceInfo.*` paths.

## `parameters`

Individual leaf declarations.

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `path` | string | (required) | Absolute parameter path. May contain a single `{i}` token if `instances` is set. |
| `type` | string | `xsd:string` | One of `xsd:string`, `xsd:int`, `xsd:unsignedInt`, `xsd:boolean`, `xsd:dateTime`, `xsd:base64`. |
| `value` | string | type-zero | The leaf's initial value (`""`, `"0"`, `"false"`, …). |
| `writable` | bool | `false` | Whether the ACS / Controller can SPV the leaf. |
| `instances` | int | (omitted) | When `path` contains `{i}`, materializes N instances (`Radio.1`, `Radio.2`, …). |
| `generator` | object | (omitted) | Inline value generator. See [Generators](../guides/generators.md). |

```yaml
parameters:
  - path: Device.WiFi.Radio.{i}.Channel
    type: xsd:unsignedInt
    instances: 2
    value: "{i}"          # → "1" / "2" at load time
    writable: true
```

## `objects` (multi-instance tables)

Sugar over the verbose form. Declare the parent path once, list the children, set `instances: N`. The loader expands to `{i}`-templated leaves and registers AddTable so `AddObject` works.

| Field | Type | Notes |
| --- | --- | --- |
| `path` | string | Parent path *without* trailing `{i}`. |
| `instances` | int | Number of instances to materialize. |
| `uniqueKeys` | list of lists | (Optional) TR-181 unique-key sets that identify rows. Each entry is a list of parameter names relative to the object. The USP `Add` handler reads matching leaves under the new instance and populates `AddResp.unique_keys` plus the autonomous `Notify(ObjectCreation).unique_keys`. Omit the block to skip the feature (empty `unique_keys` on the wire). |
| `parameters` | list | Each entry has the same fields as a top-level parameter, but `path` is relative to the parent. |

```yaml
objects:
  - path: Device.Hosts.Host
    instances: 5
    parameters:
      - path: IPAddress
        value: "192.168.1.{i}0"
      - path: HostName
        value: "host-{i}"
      - path: Active
        type: xsd:boolean
        value: "true"

  - path: Device.WiFi.SSID
    instances: 1
    uniqueKeys:
      - [SSID]
      - [BSSID]
    parameters:
      - path: SSID
        value: "Home-{i}"
        writable: true
      - path: BSSID
        value: "AABBCC11220{i}"
```

Empty key-sets (`uniqueKeys: [[]]`) reject at load time. Cross-file declarations of `uniqueKeys` for the same object reject with both filenames named.

## `groups` (single-instance prefix grouping)

For containers that aren't tables. Same shape as `objects` but no instance numbering, no AddTable registration. Each child path is concatenated as `prefix + "." + child.path`.

```yaml
groups:
  - prefix: Device.DeviceInfo.MemoryStatus
    parameters:
      - path: Total
        type: xsd:unsignedInt
        value: "262144"
      - path: Free
        type: xsd:unsignedInt
        value: "131072"
```

## `informParameters`

Per-event-code parameter lists the Inform builder includes in the `ParameterList`. The simulator picks the right list per session via **first-matching-event**.

| Key | Triggered by |
| --- | --- |
| `bootstrap` | `0 BOOTSTRAP` (first session after factory reset / fresh process). |
| `boot` | `1 BOOT` (every subsequent process start). |
| `periodic` | `2 PERIODIC` (the scheduler tick). |
| `valueChange` | `4 VALUE CHANGE` (a tracked leaf mutated). |
| `connectionRequest` | `6 CONNECTION REQUEST` (CR listener fired). |

```yaml
informParameters:
  bootstrap:
    - Device.DeviceInfo.SoftwareVersion
    - Device.IP.Interface.2.IPv4Address.1.IPAddress
  periodic:
    - Device.Ethernet.Interface.1.Stats.BytesSent
  connectionRequest:
    - Device.DeviceInfo.UpTime
```

Every path referenced here must exist in the tree; the loader rejects references to undefined leaves.

## `periodicInformPaths`

Names the two leaves the periodic Inform timer reads.

| Field | Type | Constraints |
| --- | --- | --- |
| `interval` | string | Path of an `xsd:unsignedInt` writable leaf, in seconds. |
| `enable` | string | Path of an `xsd:boolean` writable leaf. |

```yaml
periodicInformPaths:
  interval: Device.ManagementServer.PeriodicInformInterval
  enable:   Device.ManagementServer.PeriodicInformEnable
```

When omitted, the simulator exits after the bootstrap Inform (unless `--cr-bind-addr` keeps it alive). See [Periodic Inform Scheduler](../guides/scheduler.md).

## `generators` (top-level)

Generators may be declared inline on a parameter or in a top-level `generators:` list. Inline is preferred so the leaf and its mutator stay co-located. Use the top-level form when you want a generator that doesn't fit neatly inside an `objects:` / `groups:` block.

```yaml
generators:
  - path: Device.WAN.Stats.BytesSent
    type: counter
    interval: 30s
    min: 0
    max: 4294967295
    step: 12500000
    jitter: 0.2
```

| Field | Used by | Notes |
| --- | --- | --- |
| `path` | all | Absolute path to a writable leaf. |
| `type` | all | `counter` / `drift` / `enum` / `uptime` / `wallclock`. |
| `interval` | all | Tick cadence. Go duration syntax (`30s`, `5m`, …). |
| `min` | counter, drift | Lower bound. |
| `max` | counter, drift | Upper bound. |
| `step` | counter | Bytes-per-tick before jitter. |
| `jitter` | counter | Uniform fraction (`0.2` = ±20%). |
| `stepMax` | drift | Max `|delta|` per tick. |
| `values` | enum | List of strings to cycle / pick from. |
| `mode` | enum | `cycle` (default) or `random`. |

The same fields work in the inline form. See [Value Generators](../guides/generators.md) for full validation rules and behavior.

## `fleet`

Fleet metadata, named address pools, serial pattern.

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `count` | int | `1` | Number of CPEs to spawn. `0` and `1` both mean single-CPE. |
| `serialPattern` | string | `{base}-{i}` | Template applied to each CPE's SerialNumber leaf. Recognized: `{base}` (the `value:` of the SerialNumber leaf), `{i}` (1-based instance), `{i:N}` (zero-padded). |
| `pools` | map | `{}` | Named per-CPE allocators referenced from any leaf via `{name}`. |

Pool entry:

| Field | Used by | Notes |
| --- | --- | --- |
| `type` | all | `ipv4` / `ipv6` / `ipv6prefix`. |
| `cidr` | ipv4, ipv6 | Source range. |
| `super` | ipv6prefix | Operator-side super-prefix. |
| `sublen` | ipv6prefix | Per-CPE sub-prefix length. |

```yaml
fleet:
  count: 100
  serialPattern: "TEST-{i:04}"
  pools:
    wan_ipv4:
      type: ipv4
      cidr: "203.0.113.0/24"
    wan_ipv6:
      type: ipv6
      cidr: "2001:db8:1::/64"
    delegated_prefix:
      type: ipv6prefix
      super: "2001:db8:cafe::/48"
      sublen: 56
```

Pool capacity is checked at load. `fleet.count: 1001` against a `/24` (capacity 254) rejects with a precise error. See [Multi-CPE Fleets](../guides/multi-cpe.md) for the full placeholder list.

## `connectionRequest`

CR listener auth scheme and throttle window.

| Field | Type | Notes |
| --- | --- | --- |
| `scheme` | string | `""` / `basic` / `digest`. |
| `realm` | string | Required when `scheme != ""`. Sent verbatim in the WWW-Authenticate challenge. |
| `throttleWindow` | string | Go duration. `0s` / omitted disables throttling. TR-069 §3.2.2 default is `5s`. |
| `usernameParameter` | string | Tree path the listener reads per request to get the expected username. Required when `scheme != ""`. |
| `passwordParameter` | string | Tree path for the expected password. Required when `scheme != ""`. |

```yaml
connectionRequest:
  scheme: digest
  realm: cpe-sim
  throttleWindow: 5s
  usernameParameter: Device.ManagementServer.ConnectionRequestUsername
  passwordParameter: Device.ManagementServer.ConnectionRequestPassword
```

See [Connection Request Listener](../guides/connection-request.md) for full behavior.

## `transfer`

Default delay and per-FileType fault injection for the `Download` and `Upload` RPC handlers.

| Field | Type | Notes |
| --- | --- | --- |
| `defaultDelay` | string | Go duration the simulator waits before firing TransferComplete. Falls back to a code-level constant when zero / omitted. |
| `faults` | map | Keyed by `FileType` (e.g. `1 Firmware Upgrade Image`). Each entry carries `code` (BBF fault code, e.g. `9010`) and `string` (fault string). |

```yaml
transfer:
  defaultDelay: 2s
  faults:
    "1 Firmware Upgrade Image":
      code: 9010
      string: "Download failure: server unreachable"
```

When the ACS issues a `Download` whose `FileType` matches a `faults` key, the handler fires `TransferComplete` with the configured fault code and string. Useful for testing how an ACS handles failed firmware pushes.

## `eventSchedule`

Wall-clock latency between selected CWMP events and the simulated CPE's matching outbound Inform. Models the time a real CPE spends rebooting, factory-resetting, or booting up before the ACS sees the post-event Inform.

| Field | Type | Notes |
| --- | --- | --- |
| `rebootDelay` | duration | Time between a `Reboot` RPC ack and the post-reboot Inform. The deferred Inform carries `[1 BOOT, M Reboot]` (the wire shape a real CPE produces after rebooting). Repeat `Reboot` RPCs supersede the in-flight schedule. |
| `factoryResetDelay` | duration | Time between a `FactoryReset` RPC ack and the post-reset Inform. The deferred Inform carries `[1 BOOT, 0 BOOTSTRAP]` (BOOTSTRAP is re-armed by `ResetBootstrap` inside `onReset`). Errors from the deferred `onReset` are logged only — they cannot surface to the ACS because the `FactoryResetResponse` has already been sent. |
| `bootDelay` | duration | Time between process start and the per-CPE bootstrap Inform. The fleet still bootstraps in parallel; every CPE waits the same delay independently. |

```yaml
eventSchedule:
  rebootDelay: 30s
  factoryResetDelay: 60s
  bootDelay: 5s
```

All three fields are optional. Zero / unset preserves the simulator's existing immediate behavior (RPC handlers run their effect synchronously; the bootstrap Inform fires the moment the process starts). Negative values reject at load time.

`rebootDelay > 0` or `factoryResetDelay > 0` keeps the process alive long enough for the deferred Inform to fire (daemon mode). `bootDelay` alone preserves one-shot mode (the deferred bootstrap fires, then the process exits).

## `usp`

Turns the TR-369 USP role on for this CPE. When `enable: true`, cpe-labs builds an MQTT 3.1.1 transport adapter next to the existing CWMP session, derives an `os::<OUI><Serial>` endpoint ID from the profile, and emits a `Notify(OnBoardRequest)` on first contact so the controller materializes a device row. Subsequent in-process boots emit `Notify(Event{event_name: "Boot!"})` instead.

| Field | Type | Notes |
| --- | --- | --- |
| `enable` | bool | Turns the USP agent on for this device. Default `false`. |
| `endpointID.scheme` | string | TR-369 §2.2 R-ARC.2a authority scheme. Default `os`. Accepted: `oui` / `cid` / `pen` / `self` / `user` / `os` / `ops` / `uuid` / `imei` / `proto` / `doc` / `fqdn`. |
| `endpointID.ouiPath` | tree path | Leaf carrying the manufacturer OUI. Default `Device.DeviceInfo.ManufacturerOUI`. Must resolve to a `xsd:string` leaf in the merged tree. |
| `endpointID.serialPath` | tree path | Leaf carrying the device serial. Default `Device.DeviceInfo.SerialNumber`. Same type constraint. |
| `controllerEndpointID` | string | TR-369 endpoint ID of the controller. Default `self::openacs`. Must contain `::` (full TR-369 §2.2 validation runs at session startup). |
| `broker.address` | string | MQTT broker host. **Required when `enable: true`.** |
| `broker.port` | int | MQTT broker TCP port. Default `1883`. |
| `broker.protocolVersion` | string | Locked to `"3.1.1"` in v0. The NATS-native broker bridged by the reference controller does not implement MQTT 5.0. |
| `broker.username` | string | Broker auth username. Empty for anonymous (Phase A posture). |
| `broker.password` | string | Broker auth password. Empty for anonymous. |
| `broker.keepAliveSeconds` | int | MQTT KEEPALIVE. Default `60`. |
| `broker.cleanSession` | bool | MQTT CleanSession flag. Default `true`. |
| `dataModels` | []string | Data-model URIs this device supports. Default `[device]`. Advisory in v0; consumed when `GetSupportedDM` lands. |

```yaml
usp:
  enable: true
  endpointID:
    scheme: os
    ouiPath: Device.DeviceInfo.ManufacturerOUI
    serialPath: Device.DeviceInfo.SerialNumber
  controllerEndpointID: "self::openacs"
  broker:
    address: nats
    port: 1883
    protocolVersion: "3.1.1"
    keepAliveSeconds: 60
    cleanSession: true
  dataModels: [device]
```

When `enable: true`, cpe-labs installs an `Internal.Reboot.Cause` system leaf (read-only `xsd:string`, initial value `LocalFactoryReset`). The session reads it to decide between `OnBoardRequest` (first contact) and `Event{Boot!}` (subsequent boots), then flips it to `LocalReboot` after the OnBoardRequest emission. Process restart resets the leaf back to `LocalFactoryReset` because `LoadProfile` rebuilds the tree from scratch (restart-as-factory-reset, v0 semantics).

## Strict load-time validation

The loader rejects loudly. Every error names the source file and offending key:

- Unknown YAML keys at any level (`KnownFields = true`).
- Path templates malformed or containing `{i}` more than once.
- `instances: N` on a non-`{i}` path.
- Inform parameters referencing paths that don't exist in the tree.
- `periodicInformPaths` leaves with the wrong type or non-writable.
- `connectionRequest` with `scheme` set but missing `realm` / `usernameParameter` / `passwordParameter`.
- `eventSchedule` durations that don't parse via Go's `time.ParseDuration`, or that parse to a negative value.
- `fleet.pools` with a CIDR that doesn't parse, an IPv6 prefix length lower than the super-prefix length, or capacity smaller than `fleet.count`.
- Generators on the wrong leaf type (counter on a string, drift on an unsigned int).
- Two generators targeting the same path (top-level + inline on the same leaf).
- Two profile files in directory mode declaring the same singleton block (`fleet`, `transfer`, `connectionRequest`, `periodicInformPaths`, `deviceIdPaths`, `usp`).
- `usp.enable: true` with no `broker.address`.
- `usp.broker.protocolVersion` other than `"3.1.1"`.
- `usp.controllerEndpointID` without a `::` separator.
- `usp.endpointID.ouiPath` / `serialPath` referencing leaves that don't exist or aren't `xsd:string`.

Fail-fast at load beats per-CPE failure mid-bootstrap.
