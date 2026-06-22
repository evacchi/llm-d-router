# Wasm Plugin Example

Out-of-tree EPP plugin that loads WebAssembly filter and scorer modules from
an OCI registry. Includes a demo that shows live traffic shifting between pods
by hot-reloading the plugin via a ConfigMap watch.

## Structure

```
examples/wasmplugin/
  cmd/main.go              # out-of-tree EPP entry point
  Dockerfile               # builds the custom EPP image
  host/                    # wasm host plugin (filter, scorer, runtime, OCI pull, ConfigMap watch)
  guest/
    go/
      sdk/                 # TinyGo guest SDK
      plugins/             # example Go guest plugins
        demo-filter/       #   tier-based filter with build tags (standard/premium/all)
        label-filter/      #   simple label-match filter
        queue-scorer/      #   queue-depth scorer
    rust/
      sdk/                 # Rust guest SDK
      plugins/
        label-filter/      #   label-match filter in Rust
  demo/                    # demo scripts
  README.md
```

## Building the Custom EPP Image

```bash
docker build -t ghcr.io/llm-d/llm-d-router-wasm-epp:dev -f examples/wasmplugin/Dockerfile .
```

To deploy a Kind cluster with this image instead of the default EPP:

```bash
EPP_IMAGE=ghcr.io/llm-d/llm-d-router-wasm-epp:dev \
./scripts/kind-dev-env.sh
```

This uses a minimal EPP config (no tokenizer, no prefix cache) so the cluster
starts without needing to download model weights. `setup.sh` replaces the
config with the wasm demo config afterward.

Note: use `kind-dev-env.sh` directly rather than `make env-dev-kind`, which
rebuilds the default EPP image before deploying.

## Prerequisites for the Demo

- Kind cluster running with the custom EPP image (see above)
- [TinyGo](https://tinygo.org/getting-started/install/)
- [oras CLI](https://oras.land/docs/installation) (`brew install oras`)
- [guidellm](https://github.com/neuralmagic/guidellm) (`pip install guidellm`) -- for load testing

## Quick Start

```bash
# 1. Set up: registry, 2 pods with tier labels, EPP config, RBAC
./examples/wasmplugin/demo/setup.sh

# 2. Port-forward the registry (needed for oras push; re-run if it dies)
kubectl --context kind-llm-d-router-dev port-forward svc/wasm-registry 5001:5000 &

# 3. Deploy the "standard" filter -- only the standard-tier pod gets traffic
./examples/wasmplugin/demo/deploy.sh standard

# 4. Optionally deploy monitoring
./examples/wasmplugin/demo/monitoring.sh
kubectl -n monitoring port-forward svc/prometheus-grafana 3000:80 &
# Grafana: http://localhost:3000 (admin / admin)
# Open the "Wasm Plugin Demo" dashboard

# 5. Start a load test (in a separate terminal)
./examples/wasmplugin/demo/loadtest.sh

# 6. Switch to "premium" -- traffic shifts live, no restart
./examples/wasmplugin/demo/deploy.sh premium

# 7. Or unrestrict to both pods
./examples/wasmplugin/demo/deploy.sh all

# 8. Clean up
./examples/wasmplugin/demo/cleanup.sh
```

## How It Works

### Out-of-Tree Plugin

`cmd/main.go` registers `wasm-filter` and `wasm-scorer` plugin types via
`plugin.Register()` in `init()`, then runs the standard EPP via
`runner.NewRunner().Run()`. This follows the same pattern as
`examples/customscorer/`.

### Demo Filter

The demo filter (`demo-filter/`) is a TinyGo wasm module that keeps only
endpoints matching a `tier` label. Three build tags control the behavior:

| Tag | Behavior |
|-----|----------|
| `standard` | Keep only `tier=standard` pods |
| `premium` | Keep only `tier=premium` pods |
| `all` | Keep all pods (no filtering) |

The plugin logs which variant is active via `guest.LogMessage()`:

```
"msg":"wasm-filter standard: keeping standard-tier endpoints"
```

### Hot-Reload via ConfigMap Watch

The wasm-filter plugin supports a `configMapName` parameter. When set, the
plugin watches the named ConfigMap via the Kubernetes API. When the ConfigMap
changes (e.g. `deploy.sh premium` updates the OCI tag), the plugin re-pulls
the module from the registry, recompiles it, and atomically swaps it in --
no EPP restart needed.

### Monitoring

`demo/monitoring.sh` deploys kube-prometheus-stack with Grafana (5s scrape
interval) and imports a "Wasm Plugin Demo" dashboard showing per-pod request
routing rate.

Key query:

```promql
sum by (pod_name) (rate(inference_extension_scheduler_attempts_total{status="success"}[$__rate_interval]))
```

## Guest ABI

The host and guest communicate via JSON over WebAssembly linear memory. Guest
modules must be compiled as WASI reactors (`-target=wasip1 -buildmode=c-shared`
with TinyGo) so they export `_initialize` instead of `_start`.

### Exported Functions (guest -> host)

| Function | Signature | Description |
|----------|-----------|-------------|
| `alloc`  | `(size i32) -> i32` | Bump-allocate `size` bytes in the guest buffer; return pointer |
| `filter` | `(ptr i32, len i32) -> i64` | Run filter logic on JSON input at `(ptr, len)` |
| `score`  | `(ptr i32, len i32) -> i64` | Run scorer logic on JSON input at `(ptr, len)` |

A module must export `alloc` and at least one of `filter` or `score`.

The return value is a packed `uint64`: high 32 bits = result pointer, low
32 bits = result length. The host reads the JSON output from that region.

### Imported Functions (host -> guest)

| Function | Module | Signature | Description |
|----------|--------|-----------|-------------|
| `log_message` | `env` | `(ptr i32, len i32)` | Send a UTF-8 log line to the host structured logger |

### Call Protocol

1. Host serializes input to JSON.
2. Host calls guest `alloc(len)` to get a write pointer.
3. Host writes JSON bytes into guest memory at that pointer.
4. Host calls `filter(ptr, len)` or `score(ptr, len)`.
5. Host reads result JSON from the returned `(ptr, len)`.

The guest resets its bump allocator at the start of each `filter`/`score` call.

### Data Types

**Filter input:**

```json
{
  "request": {
    "request_id": "abc-123",
    "target_model": "llama-3",
    "headers": {"x-custom": "value"},
    "request_size_bytes": 1024
  },
  "endpoints": [
    {
      "id": "default/pod-1",
      "address": "10.0.0.1",
      "port": "8000",
      "labels": {"tier": "standard", "gpu-type": "a100"},
      "metrics": {
        "active_models": {"llama": 1},
        "waiting_models": {},
        "running_requests_size": 3,
        "waiting_queue_size": 5,
        "kv_cache_usage_percent": 42.5
      }
    }
  ]
}
```

**Filter output** -- list of endpoint IDs to keep:

```json
{"endpoint_ids": ["default/pod-1"]}
```

**Scorer input** -- same structure as filter input.

**Scorer output** -- map of endpoint ID to score in `[0, 1]`:

```json
{"scores": {"default/pod-1": 0.8, "default/pod-2": 0.2}}
```

Endpoint IDs are Kubernetes `namespace/name` strings
(`NamespacedName.String()`).

### Guest SDK (Go/TinyGo)

The `guest/` package provides helpers so TinyGo plugin authors don't need
to implement the ABI manually:

```go
package main

import "github.com/llm-d/llm-d-router/examples/wasmplugin/guest/go/sdk"

func init() {
    guest.RegisterFilter(func(req guest.ABIRequest, eps []guest.ABIEndpoint) []string {
        var keep []string
        for _, ep := range eps {
            if ep.Labels["tier"] == "standard" {
                keep = append(keep, ep.ID)
            }
        }
        return keep
    })
}

func main() {}
```

Build with:

```bash
tinygo build -target=wasip1 -buildmode=c-shared -no-debug -o plugin.wasm .
```

### Guest Example (Rust)

The same ABI works from any language that compiles to Wasm. See
`guest/rust/plugins/label-filter/` for a complete Rust implementation
using the Rust guest SDK (`guest/rust/sdk/`).

```bash
cd examples/wasmplugin/guest/rust/plugins/label-filter
rustup target add wasm32-wasip1
cargo build --target wasm32-wasip1 --release
```

## Demo Scripts

All scripts are under `examples/wasmplugin/demo/`:

| Script | Purpose |
|--------|---------|
| `setup.sh` | Deploy OCI registry, scale to 2 pods, label them, install EPP config, patch RBAC |
| `deploy.sh <tag>` | Build + push plugin, update ConfigMap (hot-reload, no restart) |
| `monitoring.sh` | Deploy Prometheus + Grafana, import dashboards |
| `loadtest.sh` | Run guidellm against the gateway |
| `cleanup.sh` | Remove registry, labels, ConfigMaps, temp files |
| `common.sh` | Shared variables and helpers |

## Configuration

Environment variables (set before running scripts):

| Variable | Default | Description |
|----------|---------|-------------|
| `KUBE_CONTEXT` | `kind-llm-d-router-dev` | kubectl context |
| `NAMESPACE` | `default` | Kubernetes namespace |
| `GATEWAY_PORT` | `30080` | Gateway NodePort |
| `REGISTRY_PORT` | `5001` | Local port for registry port-forward |
| `MODEL_NAME` | `TinyLlama/TinyLlama-1.1B-Chat-v1.0` | Model name for requests |
| `RATE` | `5` | Load test requests per second |
| `DURATION` | `60` | Load test duration in seconds |
