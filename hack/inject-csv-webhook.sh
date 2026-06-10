#!/usr/bin/env bash
# Injects webhookdefinitions into the generated CSV at spec.webhookdefinitions.
# operator-sdk does not generate webhookdefinitions when the webhook kustomize
# overlay is disabled (we use OLM cert injection instead of cert-manager).
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

python3 - "$CSV" "$PATCH" <<'PYEOF'
import sys, yaml

csv_path, patch_path = sys.argv[1], sys.argv[2]

with open(csv_path) as f:
    csv = yaml.safe_load(f)
with open(patch_path) as f:
    patch = yaml.safe_load(f)

csv["spec"]["webhookdefinitions"] = patch["webhookdefinitions"]

with open(csv_path, "w") as f:
    yaml.dump(csv, f, default_flow_style=False, sort_keys=False, width=200)
PYEOF

echo "Injected webhookdefinitions into $(basename "$CSV")"
