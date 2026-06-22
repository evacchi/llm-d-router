#!/usr/bin/env bash
# Shared variables and helpers for the wasm plugin demo scripts.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

: "${KUBE_CONTEXT:=kind-llm-d-router-dev}"
: "${NAMESPACE:=default}"
: "${GATEWAY_PORT:=30080}"
: "${REGISTRY_PORT:=5001}"
: "${MODEL_NAME:=TinyLlama/TinyLlama-1.1B-Chat-v1.0}"

REGISTRY_SVC="wasm-registry"
DEMO_FILTER_DIR="${ROOT_DIR}/sdk/examples/demo-filter"
EPP_CONFIG="${SCRIPT_DIR}/epp-config-wasm-demo.yaml"

info()  { echo "==> $*"; }
error() { echo "ERROR: $*" >&2; exit 1; }
