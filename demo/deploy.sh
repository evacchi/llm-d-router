#!/usr/bin/env bash
#
# Build a wasm filter plugin, push it to the in-cluster registry,
# and update the ConfigMap to trigger hot-reload (no EPP restart).
#
# Usage:
#   ./demo/deploy.sh standard   # build with "standard" tag, push, hot-reload
#   ./demo/deploy.sh premium    # build with "premium" tag, push, hot-reload
#   ./demo/deploy.sh all        # build with "all" tag, push, hot-reload
#
# Prerequisites: ./demo/setup.sh has been run (registry + port-forward up).
#
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

TAG="${1:?Usage: $0 <standard|premium|all>}"
REGISTRY_CLUSTER_ADDR="${REGISTRY_SVC}.${NAMESPACE}.svc.cluster.local:5000"

# ── Build ────────────────────────────────────────────────────────────

info "Building demo-filter (${TAG})..."
(cd "${DEMO_FILTER_DIR}" && \
    tinygo build -target=wasip1 -buildmode=c-shared -no-debug \
        -tags="${TAG}" \
        -o "${ROOT_DIR}/demo/demo-filter.wasm" .)

# ── Push with versioned tag ──────────────────────────────────────────

info "Pushing demo-filter:${TAG} to localhost:${REGISTRY_PORT}..."
(cd "${ROOT_DIR}/demo" && \
    oras push --plain-http "localhost:${REGISTRY_PORT}/demo-filter:${TAG}" \
        "demo-filter.wasm:application/vnd.wasm.content.layer.v1+wasm")

# ── Update ConfigMap to trigger hot-reload ───────────────────────────

info "Updating wasm-filter-config ConfigMap (module tag -> ${TAG})..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    delete configmap wasm-filter-config --ignore-not-found
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    create configmap wasm-filter-config \
    --from-literal=config.json="{\"module\": \"${REGISTRY_CLUSTER_ADDR}/demo-filter:${TAG}\", \"plainHTTP\": true}"

info "ConfigMap updated. EPP will hot-reload the plugin (no restart)."

# ── Verify ───────────────────────────────────────────────────────────

info "Waiting for ConfigMap propagation (~10s)..."
sleep 12

info "Sending inference request..."
curl -s -w '\n' "http://localhost:${GATEWAY_PORT}/v1/completions" \
    -H 'Content-Type: application/json' \
    -d "{\"model\":\"${MODEL_NAME}\",\"prompt\":\"hello\",\"max_tokens\":5,\"temperature\":0}" \
    | jq . || true

info "EPP logs (recent wasm lines):"
epp_pod=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get pods -l llm-d.ai/component=endpoint-picker -o name | head -1)
if [[ -n "${epp_pod}" ]]; then
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        logs "${epp_pod}" --tail=30 | grep -i "wasm" || echo "(no wasm log lines found)"
fi

echo ""
info "Deployed ${TAG}. To switch: ./demo/deploy.sh <standard|premium|all>"
