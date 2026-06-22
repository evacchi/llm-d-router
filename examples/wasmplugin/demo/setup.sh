#!/usr/bin/env bash
#
# Set up the wasm plugin demo: deploy in-cluster OCI registry, label pods,
# deploy wasm EPP config, and patch RBAC.
#
# Prerequisites:
#   - Kind cluster running with custom EPP image (see README.md)
#
# After this script finishes, start the port-forward and deploy a plugin:
#   kubectl port-forward svc/wasm-registry 5001:5000 &
#   ./examples/wasmplugin/demo/deploy.sh standard
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
fi
info "Waiting for vllm-d pods to be ready..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    rollout status deployment/vllm-d --timeout=120s

# ── Label pods ───────────────────────────────────────────────────────

info "Waiting for decode pods to be ready..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    wait --for=condition=Ready pod -l llm-d.ai/component=decode --timeout=120s

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
    i=$((i + 1))
done < <(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get pods -l llm-d.ai/component=decode \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

if [[ "${i}" -lt 2 ]]; then
    error "Expected at least 2 decode pods, found ${i}. Check vllm-d deployment."
fi

# ── EPP config ───────────────────────────────────────────────────────

info "Deploying wasm demo EPP config..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    delete configmap epp-config --ignore-not-found
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    create configmap epp-config --from-file=epp-config.yaml="${EPP_CONFIG}"

# ── Wasm filter config (watched via K8s API by the plugin) ───────────

REGISTRY_CLUSTER_ADDR="${REGISTRY_SVC}.${NAMESPACE}.svc.cluster.local:5000"

info "Creating wasm-filter-config ConfigMap..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    delete configmap wasm-filter-config --ignore-not-found
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    create configmap wasm-filter-config \
    --from-literal=config.json="{\"module\": \"${REGISTRY_CLUSTER_ADDR}/demo-filter:standard\", \"plainHTTP\": true}"

# ── Grant EPP permission to watch ConfigMaps ─────────────────────────

EPP_ROLE=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get roles -o name | grep endpoint-picker | head -1)
if [[ -n "${EPP_ROLE}" ]]; then
    # Check if the rule already exists to avoid duplicates
    has_cm=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
        get "${EPP_ROLE}" -o jsonpath='{.rules[*].resources}' | grep -c configmaps || true)
    if [[ "${has_cm}" -eq 0 ]]; then
        info "Patching ${EPP_ROLE} to allow ConfigMap watch..."
        kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" patch "${EPP_ROLE}" \
            --type=json -p '[{"op":"add","path":"/rules/-","value":{"apiGroups":[""],"resources":["configmaps"],"verbs":["get","list","watch"]}}]'
    else
        info "ConfigMap watch permission already exists, skipping."
    fi
fi

# ── Restart EPP to pick up new config ────────────────────────────────

EPP_DEPLOY=$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    get deployments -o name | grep endpoint-picker | head -1)
if [[ -z "${EPP_DEPLOY}" ]]; then
    error "Could not find EPP deployment"
fi

info "Restarting ${EPP_DEPLOY}..."
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    rollout restart "${EPP_DEPLOY}"
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" \
    rollout status "${EPP_DEPLOY}" --timeout=120s

echo ""
info "Setup complete."
info "Next:"
info "  kubectl --context ${KUBE_CONTEXT} port-forward svc/${REGISTRY_SVC} ${REGISTRY_PORT}:5000 &"
info "  ./examples/wasmplugin/demo/deploy.sh standard"
