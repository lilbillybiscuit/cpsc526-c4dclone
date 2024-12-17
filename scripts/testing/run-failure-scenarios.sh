#!/bin/bash

# Run failure scenarios
# Usage: ./run-failure-scenarios.sh <scenario-name>

SCENARIO=$1

case $SCENARIO in
    "network-partition")
        kubectl exec -it failure-agent -- /bin/sh -c "simulate-network-failure"
        ;;
    "process-crash")
        kubectl exec -it failure-agent -- /bin/sh -c "simulate-process-crash"
        ;;
    "storage-failure")
        kubectl exec -it failure-agent -- /bin/sh -c "simulate-storage-failure"
        ;;
    *)
        echo "Unknown scenario: $SCENARIO"
        echo "Available scenarios: network-partition, process-crash, storage-failure"
        exit 1
        ;;
esac
