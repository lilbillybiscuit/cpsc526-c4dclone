#!/bin/bash

minikube stop
minikube delete

minikube start \
    --nodes=3 \
    --cpus=2 \
    --memory=4096 \
    --driver=docker \
    --network-plugin=cni \
    --cni=calico \
    --container-runtime=containerd

echo "Waiting for nodes to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=300s

echo "Creating namespaces..."
kubectl create namespace c4d-system || true
kubectl create namespace compute-nodes || true

eval $(minikube docker-env)

echo "Building Docker images..."
docker build -t c4d-server:latest src/c4d/server/
docker build -t c4d-agent:latest src/c4d/agent/
docker build -t compute-node:latest src/compute/
docker build -t mvcc:latest src/mvcc/

echo "Applying Kubernetes configurations..."
kubectl apply -f configs/

echo "Deploying central components..."
kubectl apply -f deployments/central/

echo "Deploying compute nodes..."
kubectl apply -f deployments/compute/

echo "Applying network policies..."
kubectl apply -f deployments/network/

echo "Waiting for deployments to be ready..."
kubectl -n c4d-system rollout status deployment/c4d-server --timeout=300s
kubectl -n c4d-system rollout status statefulset/central-mvcc --timeout=300s
kubectl -n compute-nodes rollout status statefulset/compute-node --timeout=300s


echo "Cluster Status:"
kubectl get nodes
echo -e "\nPods Status:"
kubectl get pods --all-namespaces
