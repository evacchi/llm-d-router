# Wasm Plugin Demo

End-to-end demo of dynamic WebAssembly plugins loaded from an OCI registry.
A wasm filter plugin controls which model-server pods receive traffic. Pushing
a new plugin version to the registry and restarting the EPP shifts traffic
between pods -- visible in Grafana.

## Prerequisites

- Kind cluster running (`make env-dev-kind`)
- EPP image built with wasm support and loaded into Kind (`make image-build-epp && make image-kind`)
- [TinyGo](https://tinygo.org/getting-started/install/)
- [oras CLI](https://oras.land/docs/installation) (`brew install oras`)
- [guidellm](https://github.com/neuralmagic/guidellm) (`pip install guidellm`) -- for load testing

## Quick Start

```bash
# 1. Set up: in-cluster OCI registry, 2 model-server pods with tier labels, EPP config
./demo/setup.sh

# 2. Deploy Prometheus + Grafana
./demo/monitoring.sh
# Grafana: http://localhost:30091 (admin / admin)
# Open the "Wasm Plugin Demo" dashboard

# 3. Port-forward the registry (needed for oras push)
kubectl port-forward svc/wasm-registry 5001:5000 &

# 4. Deploy the "standard" filter -- only the standard-tier pod gets traffic
./demo/deploy.sh standard

# 5. Start a load test (in a separate terminal)
./demo/loadtest.sh

# 6. Watch Grafana -- all requests go to one pod
#    Now switch to "premium":
./demo/deploy.sh premium

# 7. Watch Grafana -- traffic shifts to the other pod
#    Or unrestrict to both:
./demo/deploy.sh all

# 8. Clean up
./demo/cleanup.sh
```

## How It Works

### Plugin

The demo plugin (`sdk/examples/demo-filter/`) is a wasm filter that keeps only
endpoints matching a `tier` label. Three build-tag variants control the behavior:

| Tag | Behavior |
|-----|----------|
| `standard` | Keep only `tier=standard` pods |
| `premium` | Keep only `tier=premium` pods |
| `all` | Keep all pods (no filtering) |

The plugin also calls `guest.LogMessage()` on each invocation, so the active
variant is visible in EPP logs:

```
"msg":"wasm-filter standard: keeping standard-tier endpoints"
```

### deploy.sh

`deploy.sh <tag>` compiles the plugin with TinyGo using the given build tag,
pushes it to the in-cluster OCI registry as `demo-filter:latest`, and restarts
the EPP. The EPP pulls the new wasm module from the registry on startup.

### Monitoring

`monitoring.sh` deploys kube-prometheus-stack with Grafana and imports two
dashboards:

- **Inference Gateway** -- the standard llm-d-router dashboard (request latency, queue sizes, etc.)
- **Wasm Plugin Demo** -- per-pod request routing rate, total throughput, and scheduling failures

The key Grafana query for observing traffic shifts:

```promql
sum by (pod_name) (rate(inference_extension_scheduler_attempts_total{status="success"}[$__rate_interval]))
```

## Scripts

| Script | Purpose |
|--------|---------|
| `setup.sh` | Deploy OCI registry, scale to 2 pods, label them, install EPP config |
| `deploy.sh <tag>` | Build + push plugin, restart EPP |
| `monitoring.sh` | Deploy Prometheus + Grafana, import dashboards |
| `loadtest.sh` | Run guidellm against the gateway |
| `cleanup.sh` | Remove registry, labels, temp files |
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
