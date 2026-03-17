#!/usr/bin/env bash
set -euo pipefail

# Removes annotation from Deployment pod template:
# spec.template.metadata.annotations["istio-operator.kyma-project.io/restartedAt"]

ANNOTATION="istio-operator.kyma-project.io/restartedAt"
# JSON Pointer escaping for "/" in annotation key
ANNOTATION_JSON_POINTER="${ANNOTATION//\//~1}"

echo "Searching for Deployments with pod-template annotation: ${ANNOTATION}..."

DEPLOYMENTS=$(kubectl get deployments --all-namespaces -o json | \
  jq -r --arg ann "$ANNOTATION" \
  '.items[]
   | select(.spec.template.metadata.annotations[$ann] != null)
   | "\(.metadata.namespace)/\(.metadata.name)"')

if [ -z "${DEPLOYMENTS}" ]; then
  echo "No Deployments found with pod-template annotation: ${ANNOTATION}"
  exit 0
fi

echo "Found the following Deployments:"
echo "${DEPLOYMENTS}"
echo ""

COUNT=0
while IFS= read -r deployment; do
  NAMESPACE="${deployment%%/*}"
  NAME="${deployment##*/}"

  echo "Removing pod-template annotation from Deployment: ${NAMESPACE}/${NAME}"
  kubectl patch deployment "${NAME}" -n "${NAMESPACE}" --type='json' \
    -p="[{'op':'remove','path':'/spec/template/metadata/annotations/${ANNOTATION_JSON_POINTER}'}]" \
    | sed "s/'/\"/g" >/dev/null

  COUNT=$((COUNT + 1))
done <<< "${DEPLOYMENTS}"

echo ""
echo "Done. Removed annotation from ${COUNT} Deployment(s)."
