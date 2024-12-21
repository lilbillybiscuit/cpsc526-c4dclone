#!/bin/bash

# Check if kubectl can connect to the cluster
if ! kubectl cluster-info >/dev/null 2>&1; then
  echo "Error: Cannot connect to Kubernetes cluster. Please check your kubeconfig and cluster status."
  exit 1
fi

echo "Starting teardown process..."

# Delete resources from distributed-system namespace
echo "Removing resources from distributed-system namespace..."
kubectl delete -f deploy/compute-pod/service.yaml -n distributed-system --ignore-not-found
kubectl delete -f deploy/compute-pod/pod.yaml -n distributed-system --ignore-not-found
kubectl delete -f deploy/compute-node/service.yaml -n distributed-system --ignore-not-found
kubectl delete -f deploy/compute-node/statefulset.yaml -n distributed-system --ignore-not-found

# Delete resources from central-services namespace
echo "Removing resources from central-services namespace..."
kubectl delete -f deploy/failure-server/service.yaml -n central-services --ignore-not-found
kubectl delete -f deploy/c4d-server/service.yaml -n central-services --ignore-not-found
kubectl delete -f deploy/task-distributor/service.yaml -n central-services --ignore-not-found
kubectl delete -f deploy/central-node/deployment.yaml -n central-services --ignore-not-found

# Uncomment if secrets were deployed
# echo "Removing secrets..."
# kubectl delete -f deploy/central-server/mvcc/secrets.yaml -n central-services --ignore-not-found
# kubectl delete -f deploy/compute-node/mvcc/secrets.yaml -n distributed-system --ignore-not-found

# Delete shared storage resources
echo "Removing shared storage resources..."
kubectl delete -f deploy/shared-storage/pvc.yaml -n distributed-system --ignore-not-found
kubectl delete -f deploy/shared-storage/pv.yaml --ignore-not-found

# Delete the namespaces
echo "Removing namespaces..."
kubectl delete namespace distributed-system --ignore-not-found
kubectl delete namespace central-services --ignore-not-found

echo "Teardown complete."
