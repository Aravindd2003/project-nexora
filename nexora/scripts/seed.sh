#!/usr/bin/env bash
# Bootstraps a demo tenant + API key by talking to Service 1 directly on
# its host-exposed port. This exists because the Gateway itself requires
# an API key to authenticate — there is a deliberate chicken-and-egg
# problem in creating the very first key, which every real platform with
# API-key auth has to solve somehow (usually a signup/console flow). Here,
# talking to the internal service directly is the equivalent of that
# signup flow for local development and grading.
#
# Usage: ./scripts/seed.sh
# Requires: docker-compose stack already running, `jq` and `curl` installed.

set -euo pipefail

CUSTOMER_ACCESS_URL="${CUSTOMER_ACCESS_URL:-http://localhost:8081}"

echo "Creating demo tenant 'Acme Support'..."
TENANT_JSON=$(curl -sf -X POST "$CUSTOMER_ACCESS_URL/internal/tenants" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Support", "tier": "standard", "rate_limit_rpm": 100, "monthly_quota": 1000, "max_concurrent_ops": 10}')

TENANT_ID=$(echo "$TENANT_JSON" | jq -r '.id')
echo "Tenant created: $TENANT_ID"

echo "Creating API key..."
KEY_JSON=$(curl -sf -X POST "$CUSTOMER_ACCESS_URL/internal/tenants/$TENANT_ID/api-keys" \
  -H "Content-Type: application/json")

RAW_KEY=$(echo "$KEY_JSON" | jq -r '.raw_key')

echo ""
echo "=========================================="
echo "  Tenant ID:  $TENANT_ID"
echo "  API Key:    $RAW_KEY"
echo "=========================================="
echo ""
echo "Next steps:"
echo "  1. Paste this key into the Customer Dashboard's API Keys page (http://localhost:3000/api-keys)"
echo "  2. Or configure the dummy customer simulator:"
echo "     curl -X POST http://localhost:4000/configure -H 'Content-Type: application/json' -d '{\"api_key\": \"$RAW_KEY\"}'"
echo "  3. Then trigger a demo scenario:"
echo "     curl -X POST http://localhost:4000/demo/normal"
