#!/usr/bin/env bash
#
# Build a wasm filter plugin, push it to the in-cluster registry,
# restart the EPP, and verify.
#
# Usage:
#   ./demo/deploy.sh standard   # build with "standard" tag, push, restart
#   ./demo/deploy.sh premium    # build with "premium" tag, push, restart
#
# Prerequisites: ./demo/setup.sh has been run (registry + port-forward up).
#
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

TAG="${1:?Usage: $0 <standard|premium>}"

# ── Build ────────────────────────────────────────────────────────────

info "Building demo-filter (${TAG})..."
(cd "${DEMO_FILTER_DIR}" && \
    tinygo build -target=wasip1 -buildmode=c-shared -no-debug \
        -tags="${TAG}" \
        -o "${ROOT_DIR}/demo/demo-filter.wasm" .)

# ── Push ─────────────────────────────────────────────────────────────

info "Pushing demo-filter:latest (${TAG}) to localhost:${REGISTRY_PORT}..."
(cd "${ROOT_DIR}/demo" && \
    oras push --plain-http "localhost:${REGISTRY_PORT}/demo-filter:latest" \
        "demo-filter.wasm:application/vnd.wasm.content.layer.v1+wasm")

# ── Restart EPP ──────────────────────────────────────────────────────

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

# ── Verify ───────────────────────────────────────────────────────────

sleep 3
info "Sending inference request..."
curl -s -w '\n' "http://localhost:${GATEWAY_PORT}/v1/completions" \
    -H 'Content-Type: application/json' \
    -d "{\"model\":\"${MODEL_NAME}\",\"prompt\":\"hello\",\"max_tokens\":5,\"temperature\":0}" \
    | jq . || true

info "EPP logs (last 20 lines with 'wasm'):"
epp_pod=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get pods -l app -o name | grep endpoint-picker | head -1)
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    logs "${epp_pod}" --tail=20 | grep -i "wasm" || echo "(no wasm log lines found)"

echo ""
info "Deployed ${TAG}. To switch: ./demo/deploy.sh <standard|premium>"
