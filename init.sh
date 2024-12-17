#!/bin/bash

# Create base directory structure
mkdir -p {deployments,src,scripts/{setup,deployment,testing,utils},docs,monitoring}

# Create source code directories
mkdir -p src/{compute-engine,c4d-agent,failure-agent,storage-engine,central-servers,common}
mkdir -p src/central-servers/{task-server,c4d-server,failure-server}

# Create deployment directories
mkdir -p deployments/{central-server,local-group,monitoring}
mkdir -p deployments/local-group/{compute-node,mvcc,network-policies}

# Create monitoring directories
mkdir -p monitoring/{prometheus,grafana}

# Create basic Dockerfile for each component
for component in compute-engine c4d-agent failure-agent storage-engine
do
cat > src/$component/Dockerfile << EOF
FROM python:3.9-slim

WORKDIR /app
COPY src/ .

RUN pip install --no-cache-dir -r requirements.txt

CMD ["python", "main.py"]
EOF

# Create basic source structure
mkdir -p src/$component/src
touch src/$component/src/{__init__.py,main.py}
touch src/$component/requirements.txt
done

# Create Makefile
cat > Makefile << 'EOF'
# Variables
KUBECTL = kubectl
KUSTOMIZE = kustomize
DOCKER = docker
NAMESPACE = distributed-system

# Docker image tags
COMPUTE_ENGINE_TAG = latest
C4D_AGENT_TAG = latest
FAILURE_AGENT_TAG = latest

# Build targets
.PHONY: build-all
build-all: build-compute build-c4d build-failure

.PHONY: build-compute
build-compute:
	$(DOCKER) build -t compute-engine:$(COMPUTE_ENGINE_TAG) src/compute-engine

.PHONY: build-c4d
build-c4d:
	$(DOCKER) build -t c4d-agent:$(C4D_AGENT_TAG) src/c4d-agent

.PHONY: build-failure
build-failure:
	$(DOCKER) build -t failure-agent:$(FAILURE_AGENT_TAG) src/failure-agent

# Deployment targets
.PHONY: deploy-all
deploy-all: deploy-central deploy-local-groups

.PHONY: deploy-central
deploy-central:
	./scripts/deployment/deploy-central.sh

.PHONY: deploy-local-group
deploy-local-group:
	./scripts/deployment/deploy-local-group.sh $(GROUP_NUM)

# Testing targets
.PHONY: test-failure
test-failure:
	./scripts/testing/run-failure-scenarios.sh

# Utility targets
.PHONY: setup-cluster
setup-cluster:
	./scripts/setup/init-cluster.sh

.PHONY: collect-logs
collect-logs:
	./scripts/utils/log-collection.sh $(NAMESPACE)

# Cleanup targets
.PHONY: cleanup
cleanup:
	$(KUBECTL) delete namespace $(NAMESPACE)
EOF

# Create deployment scripts
cat > scripts/deployment/deploy-local-group.sh << 'EOF'
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
EOF

cat > scripts/deployment/deploy-central.sh << 'EOF'
#!/bin/bash

# Deploy central services
NAMESPACE="central-services"

kubectl create namespace $NAMESPACE
kubectl apply -f deployments/central-server --namespace $NAMESPACE

echo "Central services deployed successfully"
EOF

# Create testing scripts
cat > scripts/testing/run-failure-scenarios.sh << 'EOF'
#!/bin/bash

# Run failure scenarios
# Usage: ./run-failure-scenarios.sh <scenario-name>

SCENARIO=$1

case $SCENARIO in
    "network-partition")
        kubectl exec -it failure-agent -- /bin/sh -c "simulate-network-failure"
        ;;
    "process-crash")
        kubectl exec -it failure-agent -- /bin/sh -c "simulate-process-crash"
        ;;
    "storage-failure")
        kubectl exec -it failure-agent -- /bin/sh -c "simulate-storage-failure"
        ;;
    *)
        echo "Unknown scenario: $SCENARIO"
        echo "Available scenarios: network-partition, process-crash, storage-failure"
        exit 1
        ;;
esac
EOF

# Create basic Kubernetes manifests
cat > deployments/local-group/compute-node/deployment.yaml << 'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: compute-node
spec:
  replicas: 1
  selector:
    matchLabels:
      app: compute-node
  template:
    metadata:
      labels:
        app: compute-node
    spec:
      containers:
      - name: compute-engine
        image: compute-engine:latest
      - name: c4d-agent
        image: c4d-agent:latest
      - name: failure-agent
        image: failure-agent:latest
      - name: storage-engine
        image: storage-engine:latest
EOF

cat > deployments/local-group/mvcc/statefulset.yaml << 'EOF'
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: mvcc
spec:
  replicas: 2
  selector:
    matchLabels:
      app: mvcc
  template:
    metadata:
      labels:
        app: mvcc
    spec:
      containers:
      - name: mvcc
        image: postgres:13
        env:
        - name: POSTGRES_PASSWORD
          value: mysecretpassword
EOF

# Create README
cat > README.md << 'EOF'
# Distributed System Research Project

This project implements a distributed system for research purposes, focusing on failure detection in machine learning models.

## Structure
- `deployments/`: Kubernetes manifests
- `src/`: Source code for all components
- `scripts/`: Automation scripts
- `monitoring/`: Monitoring configurations

## Quick Start

1. Build all components:
```
make build-all
```

2. Deploy central services:
```
make deploy-central
```

3. Deploy a local group:
```
make deploy-local-group GROUP_NUM=1
```

4. Run failure scenarios:
```
make test-failure
```

## Components
- Compute Engine: Main processing unit
- C4D Agent: Failure detection
- Failure Agent: Failure simulation
- Storage Engine: Data persistence
- Central Servers: Task distribution and coordination
EOF

# Make scripts executable
chmod +x scripts/deployment/deploy-local-group.sh
chmod +x scripts/deployment/deploy-central.sh
chmod +x scripts/testing/run-failure-scenarios.sh

echo "Project structure created successfully!"
echo "See README.md for usage instructions."
