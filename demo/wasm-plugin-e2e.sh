#!/usr/bin/env bash
#
# E2E demo: dynamic wasm plugin loaded from an in-cluster OCI registry.
#
# Prerequisites:
#   - Kind cluster running (make env-dev-kind)
#   - EPP image built with wasm support and loaded into Kind
#   - tinygo installed
#   - oras CLI installed (brew install oras)
#
# Usage:
#   ./demo/wasm-plugin-e2e.sh          # run full demo
#   ./demo/wasm-plugin-e2e.sh cleanup  # tear down demo resources
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

: "${KUBE_CONTEXT:=kind-llm-d-router-dev}"
: "${NAMESPACE:=default}"
: "${GATEWAY_PORT:=30080}"
: "${REGISTRY_PORT:=5001}"
: "${MODEL_NAME:=TinyLlama/TinyLlama-1.1B-Chat-v1.0}"

REGISTRY_SVC="wasm-registry"
REGISTRY_CLUSTER_ADDR="${REGISTRY_SVC}.${NAMESPACE}.svc.cluster.local:5000"
DEMO_FILTER_DIR="${ROOT_DIR}/sdk/examples/demo-filter"
EPP_CONFIG="${SCRIPT_DIR}/epp-config-wasm-demo.yaml"

info()  { echo "==> $*"; }
error() { echo "ERROR: $*" >&2; exit 1; }

wait_for_rollout() {
    local deploy="$1"
    info "Waiting for rollout of ${deploy}..."
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        rollout status "deployment/${deploy}" --timeout=120s
}

# ── Registry ─────────────────────────────────────────────────────────

deploy_registry() {
    info "Deploying in-cluster OCI registry..."
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: wasm-registry
  labels:
    app: wasm-registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: wasm-registry
  template:
    metadata:
      labels:
        app: wasm-registry
    spec:
      containers:
      - name: registry
        image: registry:2
        ports:
        - containerPort: 5000
---
apiVersion: v1
kind: Service
metadata:
  name: wasm-registry
spec:
  selector:
    app: wasm-registry
  ports:
  - port: 5000
    targetPort: 5000
EOF
    wait_for_rollout wasm-registry
}

start_port_forward() {
    info "Port-forwarding registry to localhost:${REGISTRY_PORT}..."
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        port-forward "svc/${REGISTRY_SVC}" "${REGISTRY_PORT}:5000" &
    PF_PID=$!
    sleep 2
    if ! kill -0 "${PF_PID}" 2>/dev/null; then
        error "Port-forward failed to start"
    fi
    trap 'kill ${PF_PID} 2>/dev/null || true' EXIT
}

# ── Build & Push ─────────────────────────────────────────────────────

build_and_push() {
    local version="$1"
    info "Building demo-filter (${version})..."
    (cd "${DEMO_FILTER_DIR}" && \
        tinygo build -target=wasip1 -buildmode=c-shared -no-debug \
            -tags="${version}" \
            -o "${ROOT_DIR}/demo/demo-filter.wasm" .)

    info "Pushing demo-filter:latest (${version}) to localhost:${REGISTRY_PORT}..."
    oras push --plain-http "localhost:${REGISTRY_PORT}/demo-filter:latest" \
        "${ROOT_DIR}/demo/demo-filter.wasm:application/vnd.wasm.content.layer.v1+wasm"
}

# ── Pod Labels ───────────────────────────────────────────────────────

label_pods() {
    info "Labeling model server pods..."
    local pods
    pods=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        get pods -l app -o jsonpath='{.items[*].metadata.name}')

    local i=0
    for pod in ${pods}; do
        if (( i % 2 == 0 )); then
            kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
                label pod "${pod}" tier=standard --overwrite
            info "  ${pod} -> tier=standard"
        else
            kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
                label pod "${pod}" tier=premium --overwrite
            info "  ${pod} -> tier=premium"
        fi
        ((i++))
    done
}

# ── EPP Config ───────────────────────────────────────────────────────

deploy_epp_config() {
    info "Deploying wasm demo EPP config..."
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        delete configmap epp-config --ignore-not-found
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        create configmap epp-config --from-file=epp-config.yaml="${EPP_CONFIG}"
}

restart_epp() {
    local epp_deploy
    epp_deploy=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        get deployments -o name | grep endpoint-picker | head -1)
    if [[ -z "${epp_deploy}" ]]; then
        error "Could not find EPP deployment"
    fi
    info "Restarting ${epp_deploy}..."
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        rollout restart "${epp_deploy}"
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        rollout status "${epp_deploy}" --timeout=120s
}

# ── Verify ───────────────────────────────────────────────────────────

send_request() {
    info "Sending inference request..."
    curl -s -w '\n' "http://localhost:${GATEWAY_PORT}/v1/completions" \
        -H 'Content-Type: application/json' \
        -d "{\"model\":\"${MODEL_NAME}\",\"prompt\":\"hello\",\"max_tokens\":5,\"temperature\":0}" \
        | jq . || true
}

check_epp_logs() {
    local pattern="$1"
    info "Checking EPP logs for: ${pattern}"
    local epp_pod
    epp_pod=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        get pods -l app -o name | grep endpoint-picker | head -1)
    if [[ -z "${epp_pod}" ]]; then
        error "Could not find EPP pod"
    fi
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        logs "${epp_pod}" --tail=20 | grep -i "wasm" || echo "(no wasm log lines found)"
}

# ── Cleanup ──────────────────────────────────────────────────────────

cleanup() {
    info "Cleaning up demo resources..."
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        delete deployment wasm-registry --ignore-not-found
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        delete service wasm-registry --ignore-not-found

    local pods
    pods=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        get pods -l tier -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)
    for pod in ${pods}; do
        kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
            label pod "${pod}" tier- 2>/dev/null || true
    done

    rm -f "${ROOT_DIR}/demo/demo-filter.wasm"
    info "Cleanup complete."
}

# ── Main ─────────────────────────────────────────────────────────────

if [[ "${1:-}" == "cleanup" ]]; then
    cleanup
    exit 0
fi

echo ""
echo "========================================="
echo "  Wasm Plugin E2E Demo"
echo "========================================="
echo ""

# Step 1: Deploy registry
deploy_registry
start_port_forward

# Step 2: Build and push v1
build_and_push "standard"

# Step 3: Label pods
label_pods

# Step 4: Deploy EPP config and restart
deploy_epp_config
restart_epp

# Step 5: Verify v1
echo ""
info "--- Verifying standard tier ---"
sleep 3
send_request
check_epp_logs "standard"

echo ""
echo "========================================="
echo "  Updating plugin to premium"
echo "========================================="
echo ""

# Step 6: Build and push v2
build_and_push "premium"

# Step 7: Restart EPP
restart_epp

# Step 8: Verify v2
echo ""
info "--- Verifying premium tier ---"
sleep 3
send_request
check_epp_logs "premium"

echo ""
echo "========================================="
echo "  Demo complete!"
echo "  Run './demo/wasm-plugin-e2e.sh cleanup' to tear down."
echo "========================================="
