#!/usr/bin/env bash
#
# Run a guidellm load test against the Kind cluster gateway.
#
# Prerequisites:
#   pip install guidellm
#
# Usage:
#   ./demo/loadtest.sh                    # default: 60s, rate 5
#   ./demo/loadtest.sh --rate 10 --max-seconds 120
#
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

: "${RATE:=5}"
: "${DURATION:=60}"

ENDPOINT="http://localhost:${GATEWAY_PORT}"

if ! command -v guidellm &>/dev/null; then
    error "guidellm not found. Install with: pip install guidellm"
fi

info "Running guidellm load test against ${ENDPOINT}"
info "  model:    ${MODEL_NAME}"
info "  rate:     ${RATE} req/s"
info "  duration: ${DURATION}s"
echo ""

guidellm benchmark run \
    --target "${ENDPOINT}" \
    --model "${MODEL_NAME}" \
    --data "prompt_tokens=64,output_tokens=8" \
    --rate-type constant \
    --rate "${RATE}" \
    --max-seconds "${DURATION}" \
    "$@"
