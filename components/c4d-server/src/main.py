import os
import time
import random
import threading
import requests
from flask import Flask, jsonify, request

app = Flask(__name__)

class C4DServer:
    def __init__(self):
        self.node_metrics = {}  # {node_id: {latency: [], memory_usage: []}}
        self.node_statuses = {} # {node_id: }
        self.failure_probabilities = {}  # {node_id: failure_probability}
        self.lock = threading.Lock()

    def update_node_status(self, node_id, status):
        with self.lock:
            self.node_statuses[node_id] = status

    def get_node_status(self, node_id):
        with self.lock:
            return self.node_statuses.get(node_id)

    def calculate_failure_probabilities(self):
        """Calculates failure probabilities based on collected metrics."""
        with self.lock:
            for node_id, metrics in self.node_metrics.items():
                latency = metrics["latency"]
                memory_usage = metrics["memory_usage"]

                # Simple failure probability model (you'll want to refine this)
                # Increase probability with higher latency and memory usage
                latency_score = sum(latency) / len(latency) if latency else 0
                memory_score = sum(memory_usage) / len(memory_usage) if memory_usage else 0

                failure_probability = 0.1 * latency_score + 0.005 * memory_score

                # Add a small random factor to simulate unpredictable failures
                failure_probability += random.uniform(0, 0.05)

                # Make sure the probability is within [0, 1]
                failure_probability = min(max(failure_probability, 0.0), 1.0)

                self.failure_probabilities[node_id] = failure_probability

    def collect_metrics(self):
        """Collects metrics from registered nodes."""
        while True:
            with self.lock:
                nodes_to_remove = []
                for node_id, metrics in self.node_metrics.items():
                    node_url = metrics.get("node_url")
                    if not node_url:
                        continue

                    try:
                        # Get latency
                        start_time = time.time()
                        response = requests.get(f"{node_url}/metrics", timeout=5)
                        latency = time.time() - start_time

                        if response.status_code == 200:
                            data = response.json()
                            metrics["latency"].append(latency)
                            metrics["memory_usage"].append(data["ram_usage"])

                            # Keep only the last 10 metric data points
                            metrics["latency"] = metrics["latency"][-10:]
                            metrics["memory_usage"] = metrics["memory_usage"][-10:]
                            
                            # update node status as well
                            self.update_node_status(node_id, data["compute_engine_status"])
                        else:
                            print(f"Failed to get metrics from {node_id}. Status code: {response.status_code}")
                            self.update_node_status(node_id, "unresponsive")
                            nodes_to_remove.append(node_id)
                    except requests.exceptions.RequestException as e:
                        print(f"Error collecting metrics from {node_id}: {e}")
                        self.update_node_status(node_id, "unresponsive")
                        nodes_to_remove.append(node_id)
                
                # remove nodes that are unresponsive
                for node_id in nodes_to_remove:
                    self.node_metrics.pop(node_id, None)
                    self.failure_probabilities.pop(node_id, None)

            self.calculate_failure_probabilities()
            time.sleep(10)
    
    def compute_aggregated_metrics(self):
        """Computes aggregated metrics across all nodes."""
        with self.lock:
            aggregated_metrics = {
                "total_nodes": len(self.node_metrics),
                "average_latency": 0,
                "average_memory_usage": 0,
                "failure_probabilities": {}
            }

            total_latency = 0
            total_memory_usage = 0
            node_count = len(self.node_metrics)

            if node_count > 0:
                for node_id, metrics in self.node_metrics.items():
                    total_latency += sum(metrics["latency"]) / len(metrics["latency"]) if metrics["latency"] else 0
                    total_memory_usage += sum(metrics["memory_usage"]) / len(metrics["memory_usage"]) if metrics["memory_usage"] else 0

                aggregated_metrics["average_latency"] = total_latency / node_count
                aggregated_metrics["average_memory_usage"] = total_memory_usage / node_count

            # Include failure probabilities
            aggregated_metrics["failure_probabilities"] = self.failure_probabilities.copy()

            return aggregated_metrics

    def register_node(self, node_id, node_url):
        """Registers a node with the C4D server to collect metrics."""
        with self.lock:
            self.node_metrics[node_id] = {"latency": [], "memory_usage": [], "node_url": node_url}

# Initialize the C4D server
c4d_server = C4DServer()

# Start the metric collection thread
metrics_thread = threading.Thread(target=c4d_server.collect_metrics)
metrics_thread.daemon = True
metrics_thread.start()

@app.route('/register', methods=['POST'])
def register_node():
    data = request.get_json()
    node_id = data['node_id']
    node_url = data['node_url']
    c4d_server.register_node(node_id, node_url)
    return jsonify({"message": "Node registered", "node_id": node_id})

@app.route('/failure_probabilities', methods=['GET'])
def get_failure_probabilities():
    return jsonify(c4d_server.failure_probabilities)

@app.route('/nodes/<node_id>/status', methods=['GET'])
def get_node_status(node_id):
    status = c4d_server.get_node_status(node_id)
    if status:
        return jsonify({"node_id": node_id, "status": status})
    else:
        return jsonify({"error": "Node not found"}), 404

@app.route('/aggregated_metrics', methods=['GET'])
def get_aggregated_metrics():
    """Provides aggregated metrics for all nodes."""
    aggregated_metrics = c4d_server.compute_aggregated_metrics()
    return jsonify(aggregated_metrics)

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8091)