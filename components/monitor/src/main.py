import os
import psutil
import time
import threading
import requests
import signal
from flask import Flask, jsonify, request

app = Flask(__name__)

class Monitor:
    def __init__(self):
        self.stop_event = threading.Event()
        self.ram_usage = 0
        self.cpu_usage = 0
        self.compute_engine_status = "unknown"
        self.metrics = {} # {"key": [values]}
        self.c4d_server_url = os.getenv("C4D_SERVER_URL", "http://c4d-server.central-services:8091")
        self.dist_env_vars = {
            "MASTER_ADDR": os.environ.get("MASTER_ADDR"),
            "MASTER_PORT": os.environ.get("MASTER_PORT"),
            "WORLD_SIZE": os.environ.get("WORLD_SIZE"),
            "RANK": os.environ.get("RANK"),
            "TASK_ID": os.environ.get("TASK_ID"),
        }

    def update_metrics(self):
        """Updates RAM usage, CPU usage, and compute engine status."""
        while not self.stop_event.is_set():
            self.ram_usage = psutil.virtual_memory().percent
            self.cpu_usage = psutil.cpu_percent()

            # Get compute engine status (example - you'll need to adapt this)
            try:
                with open("/tmp/compute_engine_status", "r") as f:
                    self.compute_engine_status = f.read().strip()
            except FileNotFoundError:
                self.compute_engine_status = "unknown"

            time.sleep(1)

    def append_metric(self, key,value):
        """Appends a value to the metric log."""
        if key not in self.metrics:
            self.metrics[key] = []
        self.metrics[key].append(value)
    
    def send_metrics_to_server(self):
        """Periodically sends metrics to the C4D server."""
        while not self.stop_event.is_set():
            try:
                response = requests.post(
                    f"{self.c4d_server_url}/metrics", 
                    json=self.metrics, 
                    timeout=5
                )
                if response.status_code == 200:
                    print("Metrics sent successfully to C4D server.")
                    # self.metrics = {}
                else:
                    print(f"Failed to send metrics to C4D server. Status code: {response.status_code}")
            except requests.exceptions.RequestException as e:
                print(f"Error sending metrics to C4D server: {e}")
            time.sleep(30)

    def start(self):
        """Starts the monitoring thread."""
        self.monitor_thread = threading.Thread(target=self.update_metrics)
        self.monitor_thread.start()

        self.metrics_sender_thread = threading.Thread(target=self.send_metrics_to_server)
        self.metrics_sender_thread.start()

        print("Monitor started.")

    def stop(self):
        """Stops the monitoring thread."""
        self.stop_event.set()
        self.monitor_thread.join()
        self.metrics_sender_thread.join()
        print("Monitor stopped.")

# Create a Monitor instance
monitor = Monitor()
monitor.start()

@app.route('/metrics', methods=['GET'])
def get_metrics():
    return jsonify({
        "ram_usage": monitor.ram_usage,
        "cpu_usage": monitor.cpu_usage,
        "compute_engine_status": monitor.compute_engine_status,
    })

@app.route('/env', methods=['GET'])
def get_env():
    return jsonify(
        {key: value for key, value in os.environ.items()}
    )

@app.route('/log', methods=['POST'])
def log_metric():
    data = request.get_json()
    key = data.get("key")
    value = data.get("value")
    if not key or not value:
        return jsonify({"error": "Invalid key or value."}), 400
    monitor.append_metric(key, value)
    return jsonify({"message": "Metric logged.", "key": key, "value": value})

@app.route('/dist_env', methods=['GET'])
def get_dist_env():
    return jsonify(monitor.dist_env_vars)

def shutdown_handler(signum, frame):
    print("Shutting down monitor...")
    monitor.stop()
    exit(0)

signal.signal(signal.SIGTERM, shutdown_handler)

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8081)