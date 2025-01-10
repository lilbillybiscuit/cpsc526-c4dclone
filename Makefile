KUBECTL = kubectl
DOCKER = docker
NAMESPACE = distributed-system
DOCKER_REGISTRY = lilbillybiscuit

# Docker image tags
COMPUTE_ENGINE_TAG = latest
C4D_AGENT_TAG = latest
FAILURE_AGENT_TAG = latest
FAILURE_SERVER_TAG = latest
MONITOR_TAG = latest
MVCC_TAG = latest
TASK_DISTRIBUTOR_TAG = latest

# Check if kubectl can connect to cluster
.PHONY: check-cluster
check-cluster:
	@if ! $(KUBECTL) cluster-info > /dev/null 2>&1; then \
		echo "Error: Cannot connect to Kubernetes cluster. Please check your kubeconfig and cluster status."; \
		exit 1; \
	fi

# Create all required namespaces
.PHONY: create-namespaces
create-namespaces: check-cluster
	@echo "Creating namespaces..."
	@$(KUBECTL) create namespace $(NAMESPACE) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) create namespace central-services --dry-run=client -o yaml | $(KUBECTL) apply -f -
	@$(KUBECTL) label namespace central-services name=central-services --overwrite
	@$(KUBECTL) label namespace $(NAMESPACE) name=$(NAMESPACE) --overwrite

# Build targets
.PHONY: build-all
build-all: build-compute build-c4d build-failure build-failure-server build-monitor build-mvcc build-task-distributor

.PHONY: build-compute
build-compute:
	$(DOCKER) build -t $(DOCKER_REGISTRY)/compute-engine:$(COMPUTE_ENGINE_TAG) -t compute-engine:$(COMPUTE_ENGINE_TAG) components/compute-engine

.PHONY: build-c4d
build-c4d:
    # copy components/c4d-server/proto symlink to components/c4d-server/proto2 to avoid docker build error
	cp -r components/c4d-server/proto components/c4d-server/proto2
	$(DOCKER) build -t $(DOCKER_REGISTRY)/c4d-server:$(C4D_AGENT_TAG) -t c4d-server:$(C4D_AGENT_TAG) components/c4d-server
	rm -rf components/c4d-server/proto2

.PHONY: build-failure
build-failure:
	$(DOCKER) build -t $(DOCKER_REGISTRY)/failure-agent:$(FAILURE_AGENT_TAG) -t failure-agent:$(FAILURE_AGENT_TAG) components/failure-agent

.PHONY: build-failure-server
build-failure-server:
	$(DOCKER) build -t $(DOCKER_REGISTRY)/failure-server:$(FAILURE_SERVER_TAG) -t failure-server:$(FAILURE_SERVER_TAG) components/failure-server

.PHONY: build-monitor
build-monitor:
	$(DOCKER) build -t $(DOCKER_REGISTRY)/monitor:$(MONITOR_TAG) -t monitor:$(MONITOR_TAG) components/monitor

.PHONY: build-mvcc
build-mvcc:
	$(DOCKER) build -t $(DOCKER_REGISTRY)/mvcc:$(MVCC_TAG) -t mvcc:$(MVCC_TAG) components/mvcc

.PHONY: build-task-distributor
build-task-distributor:
	$(DOCKER) build -t $(DOCKER_REGISTRY)/task-distributor:$(TASK_DISTRIBUTOR_TAG) -t task-distributor:$(TASK_DISTRIBUTOR_TAG) components/task-distributor

# Deployment targets
.PHONY: deploy
deploy: check-cluster create-namespaces deploy-central-services deploy-storage deploy-compute

.PHONY: deploy-central-services
deploy-central-services:
	@echo "Deploying central services..."
	@$(KUBECTL) apply -f deploy/central-node/deployment.yaml -n central-services
	@$(KUBECTL) apply -f deploy/task-distributor/service.yaml -n central-services
	@$(KUBECTL) apply -f deploy/c4d-server/service.yaml -n central-services
	@$(KUBECTL) apply -f deploy/failure-server/service.yaml -n central-services

.PHONY: deploy-storage
deploy-storage:
	@echo "Deploying shared storage..."
	@$(KUBECTL) apply -f deploy/shared-storage/pv.yaml
	@$(KUBECTL) apply -f deploy/shared-storage/pvc.yaml

.PHONY: deploy-compute
deploy-compute:
	@echo "Deploying compute nodes..."
	@$(KUBECTL) apply -f deploy/compute-node/statefulset.yaml -n $(NAMESPACE)
	@$(KUBECTL) apply -f deploy/compute-node/service.yaml -n $(NAMESPACE)
	@$(KUBECTL) apply -f deploy/compute-pod/pod.yaml -n $(NAMESPACE)
	@$(KUBECTL) apply -f deploy/compute-pod/service.yaml -n $(NAMESPACE)

.PHONY: deploy-secrets
deploy-secrets: check-cluster create-namespaces
	@echo "Deploying secrets..."
	@$(KUBECTL) apply -f deploy/central-server/mvcc/secrets.yaml -n central-services
	@$(KUBECTL) apply -f deploy/local-group/mvcc/secrets.yaml -n $(NAMESPACE)

# Cleanup targets
.PHONY: undeploy
undeploy: check-cluster
	@echo "Starting teardown process..."
	@echo "Removing resources from $(NAMESPACE) namespace..."
	@$(KUBECTL) delete -f deploy/compute-pod/service.yaml -n $(NAMESPACE) --ignore-not-found
	@$(KUBECTL) delete -f deploy/compute-pod/pod.yaml -n $(NAMESPACE) --ignore-not-found
	@$(KUBECTL) delete -f deploy/compute-node/service.yaml -n $(NAMESPACE) --ignore-not-found
	@$(KUBECTL) delete -f deploy/compute-node/statefulset.yaml -n $(NAMESPACE) --ignore-not-found
	@echo "Removing resources from central-services namespace..."
	@$(KUBECTL) delete -f deploy/failure-server/service.yaml -n central-services --ignore-not-found
	@$(KUBECTL) delete -f deploy/c4d-server/service.yaml -n central-services --ignore-not-found
	@$(KUBECTL) delete -f deploy/task-distributor/service.yaml -n central-services --ignore-not-found
	@$(KUBECTL) delete -f deploy/central-node/deployment.yaml -n central-services --ignore-not-found
	@echo "Removing shared storage resources..."
	@$(KUBECTL) delete -f deploy/shared-storage/pvc.yaml -n $(NAMESPACE) --ignore-not-found
	@$(KUBECTL) delete -f deploy/shared-storage/pv.yaml --ignore-not-found
	@echo "Removing namespaces..."
	@$(KUBECTL) delete namespace $(NAMESPACE) --ignore-not-found
	@$(KUBECTL) delete namespace central-services --ignore-not-found
	@echo "Teardown complete."

# Utility targets
.PHONY: cluster-status
cluster-status:
	@echo "Checking cluster connection..."
	@if $(KUBECTL) cluster-info > /dev/null 2>&1; then \
		echo "✓ Connected to Kubernetes cluster"; \
		echo "Current context: $$($(KUBECTL) config current-context)"; \
		echo "\nNamespaces:"; \
		$(KUBECTL) get namespaces; \
	else \
		echo "✗ Cannot connect to Kubernetes cluster"; \
		echo "Please check:"; \
		echo "1. Is kubectl installed?"; \
		echo "2. Is your cluster running?"; \
		echo "3. Is your kubeconfig properly configured?"; \
		echo "\nTry running: kubectl cluster-info"; \
		exit 1; \
	fi

.PHONY: status
status: check-cluster
	@echo "System Status:"
	@echo "\nNamespaces:"
	@$(KUBECTL) get namespaces
	@echo "\nCentral Services (namespace: central-services):"
	@$(KUBECTL) get all -n central-services
	@echo "\nDistributed System (namespace: $(NAMESPACE)):"
	@$(KUBECTL) get all -n $(NAMESPACE)
	@echo "\nLocal Groups:"
	@for ns in $$($(KUBECTL) get namespaces -o name | grep "^namespace/local-group-"); do \
		echo "\n$${ns#namespace/}:"; \
		$(KUBECTL) get all -n $${ns#namespace/}; \
	done
