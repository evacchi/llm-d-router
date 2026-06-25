#!/usr/bin/env bash
#
# Set up the wasm plugin demo: deploy in-cluster OCI registry, label pods,
# deploy wasm EPP config, and start a port-forward for pushing.
#
# Prerequisites:
#   - Kind cluster running (make env-dev-kind)
#   - EPP image built with wasm support and loaded into Kind
#
# After this script finishes the port-forward stays running in the background.
# Use deploy.sh to build+push a plugin version and restart the EPP.
#
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

# ── Registry ─────────────────────────────────────────────────────────

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

info "Waiting for registry rollout..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    rollout status deployment/wasm-registry --timeout=120s

# ── Scale model servers to 2 replicas ────────────────────────────────

current=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get deployment vllm-d -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
if [[ "${current}" -lt 2 ]]; then
    info "Scaling vllm-d to 2 replicas..."
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        scale deployment/vllm-d --replicas=2
    kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        rollout status deployment/vllm-d --timeout=120s
fi

# ── Label pods ───────────────────────────────────────────────────────

info "Labeling model server pods..."
i=0
while IFS= read -r pod; do
    [[ -z "${pod}" ]] && continue
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
done < <(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get pods -l llm-d.ai/component=decode -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

# ── EPP config ───────────────────────────────────────────────────────

info "Deploying wasm demo EPP config..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    delete configmap epp-config --ignore-not-found
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    create configmap epp-config --from-file=epp-config.yaml="${EPP_CONFIG}"

# ── Port-forward ─────────────────────────────────────────────────────

info "Port-forwarding registry to localhost:${REGISTRY_PORT}..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    port-forward "svc/${REGISTRY_SVC}" "${REGISTRY_PORT}:5000" &
PF_PID=$!
sleep 2
if ! kill -0 "${PF_PID}" 2>/dev/null; then
    error "Port-forward failed to start"
fi

echo ""
info "Setup complete. Port-forward running (pid ${PF_PID})."
info "Next: ./demo/deploy.sh standard"
