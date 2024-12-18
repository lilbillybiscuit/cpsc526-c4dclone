#!/bin/bash
kubectl apply -f deployments/central/
kubectl apply -f deployments/compute/
kubectl apply -f deployments/network/