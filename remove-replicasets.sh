#!/bin/bash

kubectl get rs --all-namespaces --no-headers | awk '$3 == 0 && $5 == 0 {print $1, $2}' | while read -r ns rs; do kubectl delete rs -n "$ns" "$rs"; done
