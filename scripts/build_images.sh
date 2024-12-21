#!/bin/bash

# Set Docker tag
TAG="latest"

# Function to build a Docker image
build_image() {
  local component_name=$1
  echo "Building image for ${component_name}..."
  docker build -t lilbillybiscuit/${component_name}:${TAG} -t ${component_name}:${TAG} components/${component_name}
  if [ $? -eq 0 ]; then
    echo "Image for ${component_name} built successfully."
  else
    echo "Error building image for ${component_name}."
    exit 1
  fi
}

# Build images for each component
build_image compute-engine
build_image c4d-server
build_image failure-agent
build_image failure-server
build_image monitor
build_image mvcc
build_image task-distributor

echo "All images built successfully."