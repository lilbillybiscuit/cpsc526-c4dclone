#!/bin/bash

# Start minikube
minikube start

# Build and deploy components
kubectl apply -f deploy/
