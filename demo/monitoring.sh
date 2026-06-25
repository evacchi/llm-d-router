#!/usr/bin/env bash
#
# Deploy Prometheus + Grafana into the Kind cluster and import the
# llm-d-router dashboard.
#
# After this script finishes:
#   Prometheus:  http://localhost:30090
#   Grafana:     http://localhost:30091  (admin / admin)
#
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

: "${EPP_NAME:=tinyllama-1-1b-chat-v1-0-endpoint-picker}"
: "${POOL_NAME:=tinyllama-1-1b-chat-v1-0-inference-pool}"

# ── Helm repos ───────────────────────────────────────────────────────

info "Adding prometheus-community helm repo..."
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
helm repo update prometheus-community

# ── Install kube-prometheus-stack with Grafana ───────────────────────

info "Installing Prometheus + Grafana..."
helm upgrade --install prometheus prometheus-community/kube-prometheus-stack \
    --namespace monitoring --create-namespace \
    --set alertmanager.enabled=false \
    --set kubeControllerManager.enabled=false \
    --set kubeEtcd.enabled=false \
    --set kubeProxy.enabled=false \
    --set kubeScheduler.enabled=false \
    --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
    --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false \
    --set prometheus.prometheusSpec.scrapeInterval=5s \
    --set prometheus.prometheusSpec.resources.requests.memory=512Mi \
    --set prometheus.prometheusSpec.resources.limits.memory=1Gi \
    --set prometheus.service.type=NodePort \
    --set prometheus.service.nodePort=30090 \
    --set grafana.enabled=true \
    --set grafana.service.type=NodePort \
    --set grafana.service.nodePort=30091 \
    --set grafana.adminPassword=admin \
    --kube-context "${KUBE_CONTEXT}" \
    --wait --timeout 300s

# ── ServiceMonitor / PodMonitor ──────────────────────────────────────

info "Deploying ServiceMonitor and PodMonitor..."
export EPP_NAME POOL_NAME
kubectl kustomize "${ROOT_DIR}/deploy/components/monitoring" \
    | envsubst '${EPP_NAME} ${POOL_NAME}' \
    | kubectl --context "${KUBE_CONTEXT}" apply -f -

# ── Import Grafana dashboard via provisioning ConfigMap ──────────────

info "Creating dashboard ConfigMaps..."
kubectl --context "${KUBE_CONTEXT}" -n monitoring \
    create configmap llm-d-dashboard \
        --from-file=inference-gateway.json="${ROOT_DIR}/deploy/grafana/inference_gateway.json" \
        --dry-run=client -o yaml \
    | kubectl --context "${KUBE_CONTEXT}" -n monitoring label --local -f - \
        grafana_dashboard=1 -o yaml --dry-run=client \
    | kubectl --context "${KUBE_CONTEXT}" -n monitoring apply -f -

kubectl --context "${KUBE_CONTEXT}" -n monitoring \
    create configmap wasm-demo-dashboard \
        --from-file=wasm-demo.json="${ROOT_DIR}/deploy/grafana/wasm_demo.json" \
        --dry-run=client -o yaml \
    | kubectl --context "${KUBE_CONTEXT}" -n monitoring label --local -f - \
        grafana_dashboard=1 -o yaml --dry-run=client \
    | kubectl --context "${KUBE_CONTEXT}" -n monitoring apply -f -

info "Waiting for Grafana to pick up the dashboard..."
kubectl --context "${KUBE_CONTEXT}" -n monitoring \
    rollout status deployment/prometheus-grafana --timeout=120s

echo ""
info "Monitoring deployed."
info "  Prometheus:  http://localhost:30090"
info "  Grafana:     http://localhost:30091  (admin / admin)"
