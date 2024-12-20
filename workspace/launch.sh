#!/bin/bash
RANK=$1
WORLD_SIZE=$2

# Set environment variables
export PYTHONPATH=.
export MASTER_ADDR=master  # IP address of master node
export MASTER_PORT=12355

# Launch training
python3 train.py --rank=$RANK --world_size=$WORLD_SIZE