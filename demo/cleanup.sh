#!/usr/bin/env bash
#
# Tear down wasm plugin demo resources.
#
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

info "Removing registry deployment and service..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    delete deployment wasm-registry --ignore-not-found
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    delete service wasm-registry --ignore-not-found

info "Removing tier labels from pods..."
pods=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get pods -l tier -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || true)
for pod in ${pods}; do
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        label pod "${pod}" tier- 2>/dev/null || true
done

rm -f "${ROOT_DIR}/demo/demo-filter.wasm"
info "Cleanup complete."
