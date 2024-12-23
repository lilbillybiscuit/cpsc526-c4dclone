# Stage 1: Builder
FROM ubuntu:22.04 as builder

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHON_VERSION=3.10

# Install system dependencies and Python 3.10
RUN apt-get update && apt-get install -y \
    git \
    cmake \
    build-essential \
    ninja-build \
    ccache \
    python3.10 \
    python3.10-dev \
    python3.10-venv \
    python3.10-distutils \
    python3-pip \
    gcc \
    g++ \
    gfortran \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Ensure we use Python 3.10
RUN update-alternatives --install /usr/bin/python3 python3 /usr/bin/python3.10 1 && \
    update-alternatives --set python3 /usr/bin/python3.10

# Install scikit-build and other build dependencies
RUN python3 -m pip install --no-cache-dir \
    setuptools \
    wheel \
    scikit-build \
    ninja \
    cmake

# Copy local PyTorch directory
WORKDIR /opt
COPY pytorch-c4d pytorch

# Initialize all submodules except Gloo
WORKDIR /opt/pytorch
RUN git submodule sync && \
    git submodule update --init --recursive --exclude third_party/gloo

# Handle Gloo submodule separately
ARG GLOO_CACHE_BUST=unknown
RUN echo "Gloo cache bust: $GLOO_CACHE_BUST" && \
    git submodule sync third_party/gloo && \
    git submodule update --init --recursive third_party/gloo

# Create and activate virtual environment using PyTorch's setup
RUN make setup-env

# Build PyTorch
RUN . env/bin/activate && \
    python3 setup.py install

# Stage 2: Final slim image
FROM python:3.10-slim

# Copy the built virtual environment
COPY --from=builder /opt/pytorch/env /opt/pytorch/env

# Install minimal runtime dependencies
RUN apt-get update && apt-get install -y \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Set up environment variables to use the virtual environment
ENV PATH="/opt/pytorch/env/bin:$PATH"
ENV VIRTUAL_ENV="/opt/pytorch/env"
ENV PYTHONPATH="/opt/pytorch/env/lib/python3.10/site-packages:$PYTHONPATH"

# Verify Python version matches
RUN python3 --version && \
    /opt/pytorch/env/bin/python --version

# Command to run when container starts
CMD ["python3"]
