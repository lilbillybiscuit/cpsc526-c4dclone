import os
import threading
import time
import requests
from flask import Flask, request, jsonify

app = Flask(__name__)

class TaskDistributor:
    def __init__(self, c4d_server_url):
        self.tasks = {}
        self.node_assignments = {}
        self.node_status = {}  # {node_id: status}
        self.node_failure_rates = {} # {node_id: failure_rate}
        self.task_counter = 0
        self.lock = threading.Lock()
        self.c4d_server_url = c4d_server_url

    def get_next_task_id(self):
        with self.lock:
            self.task_counter += 1
            return self.task_counter

    def update_failure_probabilities(self):
        """Periodically updates failure probabilities from the C4D server."""
        while True:
            try:
                response = requests.get(f"{self.c4d_server_url}/failure_probabilities")
                if response.status_code == 200:
                    self.node_failure_rates = response.json()
                    print(f"Updated failure probabilities: {self.node_failure_rates}")
                else:
                    print(f"Failed to get failure probabilities. Status code: {response.status_code}")
            except requests.exceptions.ConnectionError:
                print("Failed to connect to C4D server.")
            time.sleep(60)

    def register_node(self, node_id, node_url):
        """Registers a new compute node."""
        with self.lock:
            self.node_assignments[node_id] = None
            self.node_status[node_id] = "available"
            print(f"Registered node: {node_id} at {node_url}")

    def unregister_node(self, node_id):
        """Unregisters a failed node."""
        with self.lock:
            if node_id in self.node_assignments:
                # Reassign the task if there was one
                task_id = self.node_assignments[node_id]
                if task_id:
                    self.tasks[task_id]["status"] = "failed"
                    self.tasks[task_id]["node_id"] = None
                    print(f"Task {task_id} reassigned due to node failure.")
                del self.node_assignments[node_id]
            if node_id in self.node_status:
                del self.node_status[node_id]

    def get_available_node(self):
        """Gets an available node, considering failure probabilities."""
        with self.lock:
            available_nodes = [
                node_id
                for node_id, status in self.node_status.items()
                if status == "available"
            ]

            if not available_nodes:
                return None

            # Choose a node based on failure probability (lower probability is better)
            # Simple approach: choose randomly, but weight by (1 - failure_rate)
            if all(node_id in self.node_failure_rates for node_id in available_nodes):
                weights = [
                    1.0 - self.node_failure_rates.get(node_id, 0.0) for node_id in available_nodes
                ]
                # Normalize weights to sum up to 1
                total_weight = sum(weights)
                if total_weight > 0:
                    normalized_weights = [w / total_weight for w in weights]
                    chosen_node = random.choices(available_nodes, weights=normalized_weights, k=1)[0]
                else:
                    # Fallback to random choice if all weights are zero
                    chosen_node = random.choice(available_nodes)
            else:
                chosen_node = random.choice(available_nodes)

            return chosen_node

    def assign_task_to_node(self, task_id, node_id):
        """Assigns a task to a specific node."""
        with self.lock:
            self.tasks[task_id]["node_id"] = node_id
            self.node_assignments[node_id] = task_id
            self.node_status[node_id] = "busy"

    def create_task(self, task_data):
        """Creates a new task."""
        task_id = self.get_next_task_id()
        with self.lock:
            self.tasks[task_id] = {"data": task_data, "status": "pending", "node_id": None}
            return task_id

    def get_task_status(self, task_id):
        """Gets the status of a task."""
        with self.lock:
            if task_id in self.tasks:
                return self.tasks[task_id]["status"]
            else:
                return None

    def update_task_status(self, task_id, status):
        """Updates the status of a task."""
        with self.lock:
            if task_id in self.tasks:
                self.tasks[task_id]["status"] = status

    def get_node_for_task(self, task_id):
        """Finds a node for a task, prioritizing nodes already assigned to that task if available and idle."""
        with self.lock:
            task = self.tasks.get(task_id)
            if not task:
                return None

            # Check if the previously assigned node is available
            if task["node_id"]:
                previous_node_id = task["node_id"]
                if self.node_status.get(previous_node_id) == "available":
                    return previous_node_id

            # Otherwise, find a new node
            return self.get_available_node()

# Initialize the task distributor
C4D_SERVER_URL = os.environ.get("C4D_SERVER_URL", "http://c4d-server:8091")
task_distributor = TaskDistributor(C4D_SERVER_URL)

# Start the thread to update failure probabilities
update_failure_probabilities_thread = threading.Thread(target=task_distributor.update_failure_probabilities)
update_failure_probabilities_thread.daemon = True  # Allow the main program to exit even if this thread is running
update_failure_probabilities_thread.start()

@app.route('/nodes', methods=['POST'])
def register_node_endpoint():
    data = request.get_json()
    node_id = data["node_id"]
    node_url = data["node_url"]
    task_distributor.register_node(node_id, node_url)
    return jsonify({"message": "Node registered", "node_id": node_id})

@app.route('/nodes/<node_id>', methods=['DELETE'])
def unregister_node_endpoint(node_id):
    task_distributor.unregister_node(node_id)
    return jsonify({"message": f"Node {node_id} unregistered"})

@app.route('/tasks', methods=['POST'])
def create_task_endpoint():
    task_data = request.get_json()
    task_id = task_distributor.create_task(task_data)

    # Try to find a node for the task
    node_id = task_distributor.get_node_for_task(task_id)
    if node_id:
        task_distributor.assign_task_to_node(task_id, node_id)
        task_distributor.update_task_status(task_id, "running")
        return jsonify({"message": "Task created and assigned", "task_id": task_id, "node_id": node_id})
    else:
        return jsonify({"message": "Task created but no node available", "task_id": task_id}), 503

@app.route('/tasks/<task_id>/status', methods=['GET'])
def get_task_status_endpoint(task_id):
    status = task_distributor.get_task_status(int(task_id))
    if status:
        return jsonify({"task_id": task_id, "status": status})
    else:
        return jsonify({"error": "Task not found"}), 404

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8090)