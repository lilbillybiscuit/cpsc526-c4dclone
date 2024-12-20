#!/bin/bash

# Check if kubectl can connect to the cluster
if ! kubectl cluster-info >/dev/null 2>&1; then
  echo "Error: Cannot connect to Kubernetes cluster. Please check your kubeconfig and cluster status."
  exit 1
fi

# Create namespaces
echo "Creating namespaces..."
kubectl create namespace distributed-system --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace central-services --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace central-services name=central-services --overwrite
kubectl label namespace distributed-system name=distributed-system --overwrite

# Deploy central services
echo "Deploying central services..."
kubectl apply -f deploy/central-node/deployment.yaml -n central-services
kubectl apply -f deploy/task-distributor/service.yaml -n central-services
kubectl apply -f deploy/c4d-server/service.yaml -n central-services
kubectl apply -f deploy/failure-server/service.yaml -n central-services

# Deploy compute nodes
echo "Deploying compute nodes..."
kubectl apply -f deploy/compute-node/statefulset.yaml -n distributed-system
kubectl apply -f deploy/compute-node/service.yaml -n distributed-system

# Deploy a compute job
echo "Deploying a compute job..."
kubectl apply -f deploy/compute-pod/pod.yaml -n distributed-system
kubectl apply -f deploy/compute-pod/service.yaml -n distributed-system

# Deploy secrets (if needed)
# kubectl apply -f deploy/central-server/mvcc/secrets.yaml -n central-services
# kubectl apply -f deploy/compute-node/mvcc/secrets.yaml -n distributed-system

echo "Deployment complete."