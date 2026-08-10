# File Peer Discovery Plugin

**Type:** `file-peer-discovery`
**Interface:** `PeerDiscovery`

Loads peer EPP replicas from a YAML or JSON file on the local filesystem,
optionally re-loading the file when it changes. It is the Kubernetes-independent
counterpart to the EndpointSlice-based peer reconciler (`--enable-peer-discovery`).

## What It Does

Provides discovery of peer EPP replicas for cross-replica state synchronization
in deployments that run the EPP without a controller manager (bare metal, Slurm,
Ray, local development). The plugin reads a static peers file at startup, applies
each entry via `PeerNotifier`, and -- when configured to do so -- watches the
file for changes via fsnotify and reconciles the peer set on each change.

## How It Works

- **Initial load.** On `Start`, the file is read once. Each entry is validated
  (address must be a valid IPv4 literal, port must be in `[1, 65535]`) and
  applied via `notifier.Upsert`. Per-entry validation errors are logged and the
  entry is skipped; file-level problems (open, parse, size > 1 MiB) abort
  startup.
- **Reload (optional).** When `watchFile: true`, fsnotify Write / Create /
  Remove events trigger a reload. After an atomic rename or ConfigMap-style
  symlink swap (which destroys the inode being watched), the watcher is
  re-attached so subsequent changes still fire. Reload semantics match the
  initial load: invalid entries are logged and skipped, valid entries are
  applied. Peers present in the previous load but absent from the new one are
  deleted via `notifier.Delete`. A reload that fails to open or parse the file
  is logged and the previous peer set is retained.
- **Readiness.** The plugin closes its `Ready()` channel after the first
  successful load. Peer discovery does not gate request serving.

## Inputs Consumed

A YAML or JSON file with the schema below. The path is supplied via the plugin's
`path` parameter.

```yaml
peers:
  - name: <string>              # required -- unique within the file
    namespace: <string>         # optional -- defaults to "default"
    address: <IPv4>             # required -- must be a valid IPv4 address
    port: <string>              # required -- integer 1-65535 as a string
```

## Configuration

**Location:** `dataLayer.peerDiscovery.pluginRef` referencing a plugin entry of
type `file-peer-discovery` in `plugins`.
**Enabled by default:** No.

File-based peer discovery and the `--enable-peer-discovery` EndpointSlice
reconciler are mutually exclusive. The file-based plugin runs only in
file-discovery (non-Kubernetes) mode, so `dataLayer.discovery` must also be set.

### Parameters

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `path` | `string` | yes | -- | Absolute path to the peers file. |
| `watchFile` | `bool` | no | `false` | When true, watch the file for changes via fsnotify and reload on Write / Create / Remove events. |

### Examples

```yaml
plugins:
  - type: file-discovery
    name: file-discovery
    parameters:
      path: /etc/epp/endpoints.yaml
  - type: file-peer-discovery
    name: file-peer-discovery
    parameters:
      path: /etc/epp/peers.yaml
      watchFile: true
dataLayer:
  discovery:
    pluginRef: file-discovery
  peerDiscovery:
    pluginRef: file-peer-discovery
```

A two-peer file referenced by the config above:

```yaml
peers:
  - name: epp-0
    address: "10.0.0.1"
    port: "9000"
  - name: epp-1
    address: "10.0.0.2"
    port: "9000"
```

## Limitations

- The peers file is capped at 1 MiB.
- `address` must be a literal IPv4 address. Hostnames are not resolved; IPv6 is
  not supported.
- A single bad entry on initial load is logged and skipped, not fatal. If the
  entire file is not readable or fails to parse, startup fails.

## Related Documentation

- [Plugins Index](../../../README.md)
