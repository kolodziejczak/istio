#!/usr/bin/env bash
# Spawns 140 httpbin instances (each in its own namespace: httpbin-1 ... httpbin-140)

set -euo pipefail

COUNT=140

for i in $(seq 1 $COUNT); do
  NAME="httpbin-${i}"

  kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${NAME}
---
apiVersion: v1
kind: Service
metadata:
  name: ${NAME}
  labels:
    app: ${NAME}
    service: ${NAME}
spec:
  ports:
  - name: http
    port: 8000
    targetPort: 8080
  selector:
    app: ${NAME}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${NAME}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${NAME}
      version: v1
  template:
    metadata:
      labels:
        app: ${NAME}
        version: v1
    spec:
      serviceAccountName: ${NAME}
      containers:
      - image: docker.io/mccutchen/go-httpbin:v2.15.0
        imagePullPolicy: IfNotPresent
        name: httpbin
        ports:
        - containerPort: 8080
EOF

  echo "[$i/$COUNT] Deployed ${NAME} in namespace default"
done

echo "Done. $COUNT httpbin instances deployed."

