# Variables
KUBECTL = kubectl
KUSTOMIZE = kustomize
DOCKER = docker
NAMESPACE = distributed-system

# Docker image tags
COMPUTE_ENGINE_TAG = latest
C4D_AGENT_TAG = latest
FAILURE_AGENT_TAG = latest

# Check if kubectl can connect to cluster
.PHONY: check-cluster
check-cluster:
	@if ! $(KUBECTL) cluster-info > /dev/null 2>&1; then \
		echo "Error: Cannot connect to Kubernetes cluster. Please check your kubeconfig and cluster status."; \
		exit 1; \
	fi

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
.PHONY: deploy-namespace
deploy-namespace: check-cluster
	@echo "Creating central-services namespace..."
	$(KUBECTL) create namespace central-services --dry-run=client -o yaml | $(KUBECTL) apply -f -
	$(KUBECTL) label namespace central-services name=central-services --overwrite

.PHONY: deploy-secrets
deploy-secrets: check-cluster
	@echo "Deploying secrets..."
	$(KUBECTL) apply -f deployments/central-server/mvcc/secrets.yaml -n central-services
	$(KUBECTL) apply -f deployments/local-group/mvcc/secrets.yaml -n $(NAMESPACE)

.PHONY: deploy-central-services
deploy-central-services: check-cluster deploy-namespace deploy-secrets
	@echo "Deploying central services..."
	$(KUBECTL) apply -f deployments/central-server/task-server -n central-services
	$(KUBECTL) apply -f deployments/central-server/c4d-server -n central-services
	$(KUBECTL) apply -f deployments/central-server/failure-server -n central-services
	$(KUBECTL) apply -f deployments/central-server/mvcc -n central-services

.PHONY: deploy-local-group
deploy-local-group: check-cluster
	@if [ -z "$(GROUP_NUM)" ]; then \
		echo "Error: GROUP_NUM is required. Usage: make deploy-local-group GROUP_NUM=1"; \
		exit 1; \
	fi
	@echo "Deploying local group $(GROUP_NUM)..."
	$(KUBECTL) create namespace local-group-$(GROUP_NUM) --dry-run=client -o yaml | $(KUBECTL) apply -f -
	$(KUBECTL) label namespace local-group-$(GROUP_NUM) name=local-group-$(GROUP_NUM) --overwrite
	$(KUBECTL) apply -f deployments/local-group/mvcc -n local-group-$(GROUP_NUM)
	$(KUBECTL) apply -f deployments/local-group/compute-node -n local-group-$(GROUP_NUM)
	$(KUBECTL) apply -f deployments/local-group/network-policies -n local-group-$(GROUP_NUM)

# Testing targets
.PHONY: test-failure
test-failure: check-cluster
	./scripts/testing/run-failure-scenarios.sh

# Cleanup targets
.PHONY: cleanup
cleanup: check-cluster
	$(KUBECTL) delete namespace central-services --ignore-not-found
	$(KUBECTL) get namespaces -o name | grep "^namespace/local-group-" | xargs -r $(KUBECTL) delete

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
