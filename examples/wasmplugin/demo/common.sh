#!/usr/bin/env bash
# Shared variables and helpers for the wasm plugin demo scripts.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROOT_DIR="$(cd "${EXAMPLE_DIR}/../.." && pwd)"

: "${KUBE_CONTEXT:=kind-llm-d-router-dev}"
: "${NAMESPACE:=default}"
: "${GATEWAY_PORT:=30080}"
: "${REGISTRY_PORT:=5001}"
: "${MODEL_NAME:=TinyLlama/TinyLlama-1.1B-Chat-v1.0}"

REGISTRY_SVC="wasm-registry"
DEMO_FILTER_DIR="${EXAMPLE_DIR}/guest/go/plugins/demo-filter"
EPP_CONFIG="${SCRIPT_DIR}/deploy/config/epp-config-wasm-demo.yaml"

info()  { echo "==> $*"; }
error() { echo "ERROR: $*" >&2; exit 1; }
