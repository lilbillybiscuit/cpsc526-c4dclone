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
