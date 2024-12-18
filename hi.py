# init_project.py
import os
import shutil
from pathlib import Path

def create_directory_structure():
    # Base directories
    directories = [
        'configs',
        'data/shakespeare',
        'deployments/central',
        'deployments/compute',
        'deployments/network',
        'scripts',
        'src/c4d/agent',
        'src/c4d/server',
        'src/common',
        'src/compute/data/shakespeare',
        'src/failure/agent',
        'src/failure/server',
        'src/mvcc',
        'tests/integration',
        'tests/unit',
        'workspace/data/shakespeare',
    ]

    for directory in directories:
        Path(directory).mkdir(parents=True, exist_ok=True)

def create_kubernetes_manifests():
    # Central deployments
    central_manifests = {
        'deployments/central/c4d-server.yaml': '''
apiVersion: apps/v1
kind: Deployment
metadata:
  name: c4d-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: c4d-server
  template:
    metadata:
      labels:
        app: c4d-server
    spec:
      containers:
      - name: c4d-server
        image: c4d-server:latest
        ports:
        - containerPort: 8000
''',
        'deployments/central/mvcc-statefulset.yaml': '''
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: central-mvcc
spec:
  serviceName: central-mvcc
  replicas: 3
  selector:
    matchLabels:
      app: central-mvcc
''',
    }

    # Compute deployments
    compute_manifests = {
        'deployments/compute/compute-node.yaml': '''
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: compute-node
spec:
  serviceName: compute-node
  replicas: 2
  selector:
    matchLabels:
      app: compute-node
''',
    }

    # Write kubernetes manifests
    for file_path, content in {**central_manifests, **compute_manifests}.items():
        with open(file_path, 'w') as f:
            f.write(content.strip())

def create_source_files():
    # C4D Agent
    with open('src/c4d/agent/main.py', 'w') as f:
        f.write('''
import os
import time
from flask import Flask

app = Flask(__name__)

@app.route('/health')
def health():
    return {'status': 'healthy'}

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=8000)
'''.strip())

    # C4D Server
    with open('src/c4d/server/main.py', 'w') as f:
        f.write('''
import os
from flask import Flask, request

app = Flask(__name__)

@app.route('/status')
def get_status():
    return {'status': 'running'}

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=8001)
'''.strip())

    # Compute main.py
    with open('src/compute/main.py', 'w') as f:
        f.write('''
import os
import torch
import torch.distributed as dist
from model import GPT
from train import train

def main():
    # Initialize distributed training
    dist.init_process_group(backend='gloo')
    
    # Start training
    train()

if __name__ == '__main__':
    main()
'''.strip())

def create_docker_files():
    docker_files = {
        'src/c4d/agent/Dockerfile': '''
FROM python:3.9-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY . .
CMD ["python", "main.py"]
''',
        'src/c4d/server/Dockerfile': '''
FROM python:3.9-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY . .
CMD ["python", "main.py"]
''',
        'src/compute/Dockerfile': '''
FROM pytorch/pytorch:latest
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY . .
CMD ["python", "main.py"]
''',
    }

    for file_path, content in docker_files.items():
        with open(file_path, 'w') as f:
            f.write(content.strip())

def create_requirements_files():
    requirements = {
        'src/c4d/agent/requirements.txt': '''
flask==2.0.1
requests==2.26.0
''',
        'src/c4d/server/requirements.txt': '''
flask==2.0.1
pytorch==2.0.0
''',
        'src/compute/requirements.txt': '''
torch==2.0.0
numpy==1.21.0
''',
    }

    for file_path, content in requirements.items():
        with open(file_path, 'w') as f:
            f.write(content.strip())

def create_scripts():
    scripts = {
        'scripts/deploy.sh': '''
#!/bin/bash
kubectl apply -f deployments/central/
kubectl apply -f deployments/compute/
kubectl apply -f deployments/network/
''',
        'scripts/setup-cluster.sh': '''
#!/bin/bash
minikube start --nodes 3
kubectl create namespace c4d
''',
        'scripts/run-tests.sh': '''
#!/bin/bash
python -m pytest tests/
''',
    }

    for file_path, content in scripts.items():
        with open(file_path, 'w') as f:
            f.write(content.strip())
        # Make scripts executable
        os.chmod(file_path, 0o755)

def create_config_files():
    configs = {
        'configs/monitoring.yaml': '''
monitoring:
  interval: 5
  metrics:
    - cpu_usage
    - memory_usage
    - network_latency
''',
        'configs/training.yaml': '''
training:
  batch_size: 8
  learning_rate: 3e-4
  max_iterations: 1000
  model:
    n_layer: 4
    n_head: 4
    n_embd: 128
''',
    }

    for file_path, content in configs.items():
        with open(file_path, 'w') as f:
            f.write(content.strip())

def main():
    # Create directory structure
    create_directory_structure()
    
    # Create various files
    create_kubernetes_manifests()
    create_source_files()
    create_docker_files()
    create_requirements_files()
    create_scripts()
    create_config_files()
    
    # Copy existing model.py and train.py to appropriate locations
    if os.path.exists('model.py'):
        shutil.copy('model.py', 'src/compute/model.py')
    if os.path.exists('train.py'):
        shutil.copy('train.py', 'src/compute/train.py')
    
    # Create README.md
    with open('README.md', 'w') as f:
        f.write('''
# C4D Implementation

This is an implementation of the C4D (Communication-Driven) approach for distributed training.

## Structure
- `configs/`: Configuration files
- `deployments/`: Kubernetes deployment manifests
- `src/`: Source code
  - `c4d/`: C4D server and agent implementation
  - `compute/`: Compute node implementation
  - `failure/`: Failure injection system
  - `mvcc/`: MVCC implementation
- `tests/`: Unit and integration tests

## Setup
1. Run `scripts/setup-cluster.sh` to set up the Kubernetes cluster
2. Run `scripts/deploy.sh` to deploy the system

## Usage
See documentation for detailed usage instructions.
'''.strip())

if __name__ == '__main__':
    main()

