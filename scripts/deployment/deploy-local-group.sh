#!/bin/bash

# Deploy a local group to the cluster
# Usage: ./deploy-local-group.sh <group-number>

GROUP_NUM=$1

if [ -z "$GROUP_NUM" ]; then
    echo "Usage: ./deploy-local-group.sh <group-number>"
    exit 1
fi

# Create namespace for the local group
NAMESPACE="local-group-${GROUP_NUM}"
kubectl create namespace $NAMESPACE

# Apply configurations
kubectl apply -f deployments/local-group --namespace $NAMESPACE

# Wait for pods to be ready
kubectl wait --for=condition=ready pods \
    --namespace $NAMESPACE \
    --selector=app=compute-node \
    --timeout=300s

echo "Local group $GROUP_NUM deployed successfully"
