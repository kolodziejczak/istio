#!/usr/bin/env bash

# remove all stale replicasets
# kubectl get rs -n test --no-headers | awk '$2 == 0 && $4 == 0 {print $1}' | xargs -r kubectl delete rs -n test

# KWOK repository
KWOK_REPO=kubernetes-sigs/kwok
# Get latest
KWOK_LATEST_RELEASE=$(curl "https://api.github.com/repos/${KWOK_REPO}/releases/latest" | jq -r '.tag_name')
kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml"
kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml"


echo "Creating $nodes fake Nodes..."
for i in {1..60}; do
  kubectl apply -f - <<EOF
    apiVersion: v1
    kind: Node
    metadata:
      annotations:
        node.alpha.kubernetes.io/ttl: "0"
        kwok.x-k8s.io/node: fake
      labels:
        beta.kubernetes.io/arch: amd64
        beta.kubernetes.io/os: linux
        kubernetes.io/arch: amd64
        kubernetes.io/hostname: kwok-node-$i
        kubernetes.io/os: linux
        kubernetes.io/role: agent
        node-role.kubernetes.io/agent: ""
        type: kwok
      name: kwok-node-$i
    spec:
      taints: # Avoid scheduling actual running pods to fake Node
      - effect: NoSchedule
        key: kwok.x-k8s.io/node
        value: fake
    status:
      allocatable:
        cpu: 32
        memory: 256Gi
        pods: 110
      capacity:
        cpu: 32
        memory: 256Gi
        pods: 110
      nodeInfo:
        architecture: amd64
        bootID: ""
        containerRuntimeVersion: ""
        kernelVersion: ""
        kubeProxyVersion: "fake"
        kubeletVersion: v1.34.3
        machineID: ""
        operatingSystem: linux
        osImage: "Debian GNU/Linux 12 (bookworm)"
        systemUUID: ""
      phase: Running
EOF
done

kubectl create ns test
kubectl label ns test istio-injection=enabled --overwrite

echo "create $deployments deployments"
for i in {1..150}; do
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fake-pod-$i
  namespace: test
spec:
  replicas: 2
  selector:
    matchLabels:
      app: fake-pod-$i
  template:
    metadata:
      labels:
        app: fake-pod-$i
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: type
                operator: In
                values:
                - kwok
      # A taints was added to an automatically created Node.
      # You can remove taints of Node or add this tolerations.
      tolerations:
      - key: "kwok.x-k8s.io/node"
        operator: "Exists"
        effect: "NoSchedule"
      containers:
      - name: fake-container
        image: fake-image
EOF
done