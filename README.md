# C4D Research Project

Implementation of failure detection and recovery system.

## Structure
- `components/`: Individual system components
- `deploy/`: Kubernetes deployment files
- `results/`: Experiment results
- `tests/`: Unit and integration tests
- `utils/`: Shared utilities
- `scripts/`: Helper scripts

There is also an external repository for the CCL library, located here:

https://github.com/lilbillybiscuit/pytorch-c4d

Which has a submodule at:
https://github.com/lilbillybiscuit/gloo-c4d

The method to compile this library is the standard way to compile any Pytorch library.

To make things easier, use the tag lilbillybiscuit/pytorch-c4d:latest when building using the dockerfile located in `utils/custom-pytorch-build/Dockerfile` to use the provided Kubernetes images.

## Components
Light documentation for all of the components exists in their respective directories


## Contributions

Bill Qian contributed to the development of the Kubernetes infrastructure, including the deployment scripts, the scheduling workflow, the process wrapper for compute, and the monitoring agent. Furthermore, he designed the CCL enhancements and implemented communication across various channels in the system. Finally, he helped set up the final multi-node Kubernetes cluster and optimized for performance.

Jewon Im contributed to the implementation of the monitoring agent and server, failure detection, and the sample GPT training script used in experiments. Furthermore, she helped implement some of the CCL functions and the log reporting. Lastly, she helped set up the final multi-node Kubernetes cluster and ran experiments.


## Running the system

First, build the pytorch-c4d image. There is an ARM64 image currently on Docker hub, but it is recommended to build the image locally to ensure compatibility.

Next, set up a kubernetes cluster using any service (we set up our own using EC2 instances) with some shared network file system between physical hosts (we used EFS).

Then, install kubectl, and install the Pytorch training operator:
```
kubectl apply --server-side -k "github.com/kubeflow/training-operator.git/manifests/overlays/standalone?ref=v1.8.1"
```

Then, run the following command to deploy the system:
```
make build-all
make deploy
```

The training progress can be inspected by watching the container logs. Failures can be simulated by manually running exec into a node, and using the `tc` command