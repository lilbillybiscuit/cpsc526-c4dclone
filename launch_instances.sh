#!/bin/bash

# Usage: ./launch_instances.sh <num_instances> [stf_values...]
# Example: ./launch_instances.sh 3 10 15 20

if [ $# -lt 1 ]; then
    echo "Usage: ./launch_instances.sh <num_instances> [stf_values...]"
    exit 1
fi


make -C "components/monitor" build

NUM_INSTANCES=$1
shift  # Remove first argument, leaving STF values in $@

# Check if we have enough STF values
if [ $# -ne $NUM_INSTANCES ]; then
    echo "Error: Please provide exactly $NUM_INSTANCES STF values"
    exit 1
fi

# Convert remaining arguments to array
STF_VALUES=("$@")

# Create a new tmux session
tmux new-session -d -s c4d_instances

# Base ports
BASE_PORT=10024
BASE_AGENT_PORT=10124

# Launch instances
for ((i=0; i<$NUM_INSTANCES; i++)); do
    # Calculate ports for this instance
    PORT=$((BASE_PORT + i))
    AGENT_PORT=$((BASE_AGENT_PORT + i))
    
    # Get the STF value for this instance
    STF_VALUE=${STF_VALUES[$i]}
    
    # Create new window for this instance
    WINDOW_NAME="instance_$i"
    tmux new-window -t c4d_instances -n $WINDOW_NAME
    
    # Split window vertically
    tmux split-window -t c4d_instances:$WINDOW_NAME -v
    tmux split-window -t c4d_instances:$WINDOW_NAME -v
    
    # Send commands to each pane
    # Monitor
    tmux send-keys -t c4d_instances:$WINDOW_NAME.0 "cd /Users/billyqian/Projects/YALE_CS/CPSC426/cpsc526-c4dclone/components/monitor" C-m
    tmux send-keys -t c4d_instances:$WINDOW_NAME.0 "TASK_ID=127.0.0 NAMESPACE=1 PORT=$PORT AGENT_PORT=$AGENT_PORT bin/monitor" C-m
    
    # Test script
    tmux send-keys -t c4d_instances:$WINDOW_NAME.1 "cd /Users/billyqian/Projects/YALE_CS/CPSC426/cpsc526-c4dclone/components/monitor" C-m
    tmux send-keys -t c4d_instances:$WINDOW_NAME.1 "NUM_RANKS=$NUM_INSTANCES PORT=$PORT python python_test/test.py" C-m
    
    # Compute engine
    tmux send-keys -t c4d_instances:$WINDOW_NAME.2 "cd /Users/billyqian/Projects/YALE_CS/CPSC426/cpsc526-c4dclone/components/compute-engine/user_code" C-m
    tmux send-keys -t c4d_instances:$WINDOW_NAME.2 "STF=$STF_VALUE AGENT_PORT=$AGENT_PORT cargo run -r" C-m
done

# Attach to the tmux session
tmux attach-session -t c4d_instances
