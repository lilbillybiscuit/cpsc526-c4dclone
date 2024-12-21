#!/bin/bash

# check if minikube is installed and running
if ! minikube status >/dev/null 2>&1; then
  echo "Error: Minikube is not running. Please start Minikube before running this script."
  exit 1
fi

# create a shared directory for Minikube
mkdir -p ./minikube-shared/checkpoints
minikube mount ./minikube-shared:/mnt/minikube-shared &