import requests
import random
import time
import json
from datetime import datetime
import threading
from typing import Dict, List

import os
port = os.environ.get("PORT", 8081)
num_ranks_env = os.getenv("NUM_RANKS")
num_ranks = int(num_ranks_env) if num_ranks_env else 4

class CCLSimulator:
    def __init__(self, url: str = f"http://localhost:{port}/log_ccl"):
        self.url = url
        self.running = True
        self.operation_counter = 0

        # Simulation parameters
        self.num_ranks = num_ranks
        self.current_time_us = int(time.time() * 1_000_000)

        # Common operation types and configurations
        self.collective_ops = ["allreduce", "broadcast", "allgather", "reduce"]
        self.algorithms = ["ring", "butterfly", "pairwise", "bcube"]
        self.datatypes = ["float32", "float64", "int32", "int64"]

        # Track ongoing operations for completion
        self.ongoing_operations: Dict[str, Dict] = {}

    def get_random_bytes(self) -> int:
        """Generate random message sizes between 1KB and 1GB"""
        return random.randint(1024, 1024 * 1024 * 1024)

    def get_random_count(self) -> int:
        """Generate random element counts"""
        return random.randint(100, 1000000)

    def generate_collective_operation(self) -> Dict:
        """Generate a random collective operation event"""
        op_type = random.choice(self.collective_ops)
        rank = random.randint(0, self.num_ranks - 1)

        event = {
            "opType": op_type,
            "algorithm": random.choice(self.algorithms),
            "dataType": random.choice(self.datatypes),
            "count": self.get_random_count(),
            "rootRank": random.randint(0, self.num_ranks - 1) if op_type in ["broadcast", "reduce"] else -1,
            "context_rank": rank,
            "context_size": self.num_ranks,
            "startTime": self.current_time_us,
            "endTime": 0,
            "filename": f"{op_type}_op.cpp",
            "finished": False
        }

        return event

    def generate_p2p_operation(self) -> Dict:
        """Generate a random point-to-point operation event"""
        op_type = random.choice(["send", "recv"])
        rank = random.randint(0, self.num_ranks - 1)

        # Ensure remote rank is different from current rank
        remote_rank = rank
        while remote_rank == rank:
            remote_rank = random.randint(0, self.num_ranks - 1)

        event = {
            "opType": op_type,
            "remoteRank": remote_rank,
            "context_rank": rank,
            "context_size": self.num_ranks,
            "bytes": self.get_random_bytes(),
            "startTime": self.current_time_us,
            "endTime": 0,
            "filename": "p2p_comm.cpp",
            "finished": False
        }

        return event

    def send_event(self, event: Dict):
        """Send event to monitoring server"""
        try:
            headers = {
                "Content-Type": "application/json",
                "Accept": "application/json"
            }
            response = requests.post(self.url, json=event, headers=headers)
            if response.status_code != 200:
                print(f"Failed to send event: {response.status_code}")
        except Exception as e:
            print(f"Error sending event: {e}")

    def complete_operation(self, event: Dict) -> Dict:
        """Generate completion event for an operation"""
        completion_event = event.copy()
        # Random duration between 100us and 1s
        if os.getenv("FAIL", "false").lower() == "true":
            # Start increasing latency after 5 seconds
            elapsed_time = time.time() - start_time
            if elapsed_time > 5:
                current_latency = 0.4 + (elapsed_time - 5) * 0.1
                completion_event["endTime"] = event["startTime"] + int(current_latency * 1_000_000)
            else:
                completion_event["endTime"] = event["startTime"] + int(0.4 * 1_000_000)
        else:
            completion_event["endTime"] = event["startTime"] + int(0.4 * 1_000_000)
        completion_event["finished"] = True
        return completion_event

    def simulation_loop(self):
        """Main simulation loop"""
        start_time = time.time()
        while self.running:
            # Generate either collective or p2p operation
            event = (self.generate_collective_operation()
                    if random.random() < 0.7
                    else self.generate_p2p_operation())

            # Send start event
            self.send_event(event)

            # Schedule completion
            completion_delay = random.uniform(0.001, 0.1)  # 1ms to 100ms
            time.sleep(completion_delay)

            # Send completion event
            completion_event = self.complete_operation(event)
            self.send_event(completion_event)

            # Update simulation time
            self.current_time_us += int(completion_delay * 1_000_000)

            # Small delay between operations
            time.sleep(random.uniform(0.01, 0.5))  # 10ms to 500ms between operations

def main():
    simulator = CCLSimulator()

    try:
        # Start multiple simulation threads to simulate multiple nodes
        threads = []
        for _ in range(3):  # Run 3 parallel simulators
            thread = threading.Thread(target=simulator.simulation_loop)
            thread.daemon = True
            thread.start()
            threads.append(thread)

        # Keep main thread running
        while True:
            time.sleep(1)

    except KeyboardInterrupt:
        print("\nStopping simulation...")
        simulator.running = False
        for thread in threads:
            thread.join()
        print("Simulation stopped")

if __name__ == "__main__":
    start_time = time.time()
    main()
