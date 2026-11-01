#!/usr/bin/env bash
# Injects webhookdefinitions into the generated CSV at spec.webhookdefinitions.
# operator-sdk does not generate webhookdefinitions when the webhook kustomize
# overlay is disabled (we use OLM cert injection instead of cert-manager).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

CSV=$(find "${ROOT_DIR}/bundle/manifests" -maxdepth 1 -name '*clusterserviceversion.yaml' -print -quit)
PATCH="${ROOT_DIR}/config/manifests/patches/webhook.yaml"
SOURCE_CSV="${ROOT_DIR}/config/manifests/bases/kubernaut-operator.clusterserviceversion.yaml"

if [[ ! -f "$CSV" ]]; then
  echo "ERROR: CSV not found in bundle/manifests/" >&2
  exit 1
fi

if [[ ! -f "$PATCH" ]]; then
  echo "ERROR: webhook patch not found at ${PATCH}" >&2
  exit 1
fi

if [[ ! -f "$SOURCE_CSV" ]]; then
  echo "ERROR: source CSV not found at ${SOURCE_CSV}" >&2
  exit 1
fi

python3 - "$CSV" "$PATCH" "$SOURCE_CSV" <<'PYEOF'
import sys, yaml

csv_path, patch_path, source_csv_path = sys.argv[1], sys.argv[2], sys.argv[3]

with open(csv_path) as f:
    csv = yaml.safe_load(f)
with open(patch_path) as f:
    patch = yaml.safe_load(f)
with open(source_csv_path) as f:
    source_csv = yaml.safe_load(f)

if "webhookdefinitions" not in csv["spec"]:
    csv["spec"]["webhookdefinitions"] = patch["webhookdefinitions"]
csv["metadata"]["annotations"]["createdAt"] = source_csv["metadata"]["annotations"]["createdAt"]

with open(csv_path, "w") as f:
    yaml.dump(csv, f, default_flow_style=False, sort_keys=False, width=200)
PYEOF

echo "Injected webhookdefinitions into $(basename "$CSV")"
