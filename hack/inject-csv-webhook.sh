#!/usr/bin/env bash
# Injects webhookdefinitions into the generated CSV.
# operator-sdk does not generate webhookdefinitions when the webhook kustomize
# overlay is disabled (we use OLM cert injection instead of cert-manager),
# so this script splices the definition from config/manifests/patches/webhook.yaml
# into spec.install.spec right before the "strategy: deployment" line.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

CSV=$(ls "${ROOT_DIR}"/bundle/manifests/*clusterserviceversion.yaml 2>/dev/null | head -1)
PATCH="${ROOT_DIR}/config/manifests/patches/webhook.yaml"

if [[ ! -f "$CSV" ]]; then
  echo "ERROR: CSV not found in bundle/manifests/" >&2
  exit 1
fi

if [[ ! -f "$PATCH" ]]; then
  echo "ERROR: webhook patch not found at ${PATCH}" >&2
  exit 1
fi

if grep -q 'webhookdefinitions:' "$CSV"; then
  echo "webhookdefinitions already present in CSV — skipping injection"
  exit 0
fi

TMPFILE=$(mktemp)
sed 's/^/    /' "$PATCH" > "$TMPFILE"

# Use sed to insert the indented patch before "    strategy: deployment"
# macOS sed requires slightly different syntax
if sed --version 2>/dev/null | grep -q GNU; then
  sed -i "/^    strategy: deployment$/r ${TMPFILE}" "$CSV"
else
  sed -i '' "/^    strategy: deployment$/r ${TMPFILE}" "$CSV"
fi

rm -f "$TMPFILE"
echo "Injected webhookdefinitions into $(basename "$CSV")"
