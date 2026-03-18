kubectl get deployments -A -o json | jq -r '.items[] | select(.spec.template.metadata.annotations["istio-operator.kyma-project.io/restartedAt"] == null) | "\(.metadata.namespace)/\(.metadata.name)"'
