import os
import stat

def create_file(path, content=''):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, 'w') as f:
        f.write(content)

def create_project_structure():
    # Root directory
    root = '.'
    os.makedirs(root, exist_ok=True)

    # Basic templates
    dockerfile_template = '''FROM python:3.9-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY src/ .
CMD ["python", "main.py"]
'''

    requirements_template = '''numpy
pandas
pytorch
kubernetes
'''

    kubernetes_service_template = '''apiVersion: v1
kind: Service
metadata:
  name: {name}
spec:
  selector:
    app: {name}
  ports:
    - port: 8080
      targetPort: 8080
'''

    # File structure with content
    files = {
        # Deploy files
        f'{root}/deploy/compute-pod/pytorchjob.yaml': '''apiVersion: "kubeflow.org/v1"
kind: PyTorchJob
metadata:
  name: c4d-training-job
spec:
  pytorchReplicaSpecs:
    Worker:
      replicas: 3
      template:
        spec:
          containers:
          - name: pytorch
            image: your-compute-image
''',
        f'{root}/deploy/compute-node/statefulset.yaml': '''apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: compute-node
spec:
  serviceName: compute-node
  replicas: 2
''',
        f'{root}/deploy/central-node/deployment.yaml': '''apiVersion: apps/v1
kind: Deployment
metadata:
  name: central-node
spec:
  replicas: 1
''',
    }

    # Components
    components = ['compute-engine', 'monitor', 'failure-agent', 'mvcc', 
                 'task-distributor', 'c4d-server']
    
    for component in components:
        base_path = f'{root}/components/{component}'
        files.update({
            f'{base_path}/Dockerfile': dockerfile_template,
            f'{base_path}/requirements.txt': requirements_template,
            f'{base_path}/src/main.py': f'# Main {component} implementation\n',
            f'{base_path}/README.md': f'# {component}\n\nDescription of {component} component.',
        })
        
        # Add service.yaml for each component
        files[f'{root}/deploy/{component}/service.yaml'] = kubernetes_service_template.format(
            name=component
        )

    # Experiments
    files.update({
        f'{root}/experiments/configs/baseline.yaml': '''experiment:
  name: baseline
  parameters:
    batch_size: 32
    learning_rate: 0.001
''',
        f'{root}/experiments/configs/failure_scenarios.yaml': '''scenarios:
  - name: random_node_failure
    probability: 0.1
''',
        f'{root}/experiments/scripts/run_experiment.py': '# Script to run experiments\n',
        f'{root}/experiments/scripts/analyze_results.py': '# Script to analyze results\n',
    })

    # Notebooks
    files.update({
        f'{root}/notebooks/failure_analysis.ipynb': '{"cells": [], "metadata": {}, "nbformat": 4, "nbformat_minor": 4}',
        f'{root}/notebooks/performance_metrics.ipynb': '{"cells": [], "metadata": {}, "nbformat": 4, "nbformat_minor": 4}',
    })

    # Tests
    files.update({
        f'{root}/tests/unit/__init__.py': '',
        f'{root}/tests/integration/__init__.py': '',
    })

    # Utils
    files.update({
        f'{root}/utils/__init__.py': '',
        f'{root}/utils/logging.py': '''import logging

def setup_logger(name):
    logger = logging.getLogger(name)
    return logger
''',
        f'{root}/utils/metrics.py': '''def calculate_metrics(results):
    """Calculate performance metrics."""
    pass
''',
    })

    # Root level files
    files.update({
        f'{root}/README.md': '''# C4D Research Project

Implementation of failure detection and recovery system.

## Structure
- `components/`: Individual system components
- `deploy/`: Kubernetes deployment files
- `experiments/`: Experiment configurations and scripts
- `notebooks/`: Analysis notebooks
- `results/`: Experiment results
- `tests/`: Unit and integration tests
- `utils/`: Shared utilities
''',
        f'{root}/requirements.txt': '''numpy
pandas
pytorch
kubernetes
jupyter
pytest
''',
        f'{root}/setup.py': '''from setuptools import setup, find_packages

setup(
    name="c4d-research",
    version="0.1",
    packages=find_packages(),
    install_requires=[
        "numpy",
        "pandas",
        "pytorch",
        "kubernetes",
    ],
)
''',
        f'{root}/.gitignore': '''*.pyc
__pycache__/
.pytest_cache/
*.egg-info/
.ipynb_checkpoints/
results/raw/*
results/processed/*
.env
''',
    })

    # Create all directories and files
    for file_path, content in files.items():
        create_file(file_path, content)

    # Create empty results directories
    os.makedirs(f'{root}/results/raw', exist_ok=True)
    os.makedirs(f'{root}/results/processed', exist_ok=True)

    # Create development script
    dev_script = f'{root}/dev.sh'
    create_file(dev_script, '''#!/bin/bash

# Start minikube
minikube start

# Build and deploy components
kubectl apply -f deploy/
''')
    
    # Make dev.sh executable
    os.chmod(dev_script, os.stat(dev_script).st_mode | stat.S_IEXEC)

if __name__ == '__main__':
    create_project_structure()
    print("Project structure created successfully!")
