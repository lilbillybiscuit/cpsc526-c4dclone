#!/bin/bash

# Deploy central services
NAMESPACE="central-services"

kubectl create namespace $NAMESPACE
kubectl apply -f deployments/central-server --namespace $NAMESPACE

echo "Central services deployed successfully"
