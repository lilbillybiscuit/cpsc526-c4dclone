import os
import psutil
import time
import threading
import requests
import signal
import random
from flask import Flask, jsonify, request

app = Flask(__name__)


class Monitor:
    def __init__(self):
        self.stop_event = threading.Event()
        self.ram_usage = 0
        self.cpu_usage = 0
        self.compute_engine_status = "unknown"
        self.metrics = {}  # {"key": [values]}
        self.c4d_server_url = os.getenv("C4D_SERVER_URL", "http://c4d-server.central-services:8091")
        self.is_standby = random.random() < 0.25
        self.node_status = "active" if not self.is_standby else "standby"
        self.dist_env_vars = {
            "MASTER_ADDR": os.environ.get("MASTER_ADDR"),
            "MASTER_PORT": os.environ.get("MASTER_PORT"),
            "WORLD_SIZE": os.environ.get("WORLD_SIZE"),
            "RANK": os.environ.get("RANK"),
            "TASK_ID": os.environ.get("TASK_ID"),
        }

        self.ccl_logs = []
        self.ccl_lock = threading.Lock()

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

    def append_metric(self, key, value):
        """Appends a value to the metric log."""
        if key not in self.metrics:
            self.metrics[key] = []
        self.metrics[key].append(value)

    def send_metrics_to_server(self):
        response = requests.get(f"{self.c4d_server_url}/", timeout=5)
        latency = response.elapsed.total_seconds()
        while not self.stop_event.is_set():
            ccl_logs = self.get_ccl_logs()

            if not ccl_logs:  # Skip if no logs
                time.sleep(2)
                continue

            # Group CCL logs by operation type
            grouped_logs = {}
            for log in ccl_logs:
                op_type = log["opType"]
                if op_type not in grouped_logs:
                    grouped_logs[op_type] = []
                grouped_logs[op_type].append(log)

            payload = {
                "node_id": os.getenv("TASK_ID", "unknown"),
                "metrics": {
                    "cpu_usage": [self.cpu_usage],
                    "ram_usage": [self.ram_usage],
                    "latency": [latency]
                }
            }

            # Add CCL metrics if any exist
            if grouped_logs:
                payload["metrics"]["ccl_operations"] = grouped_logs

            try:
                response = requests.post(
                    f"{self.c4d_server_url}/metrics",
                    json=payload,
                    headers={"Content-Type": "application/json"},
                    timeout=10
                )
                if response.status_code == 200:
                    print("Metrics successfully sent to C4D server.")
                    # Only clear logs after successful transmission
                    self.clear_ccl_logs()
                else:
                    print(f"Failed to send metrics to C4D server. Status code: {response.status_code}")
                latency = response.elapsed.total_seconds()
            except requests.RequestException as e:
                print(f"Error sending metrics to C4D server: {e}")
                # Logs will be retried on next iteration

            time.sleep(2)

    def register_with_server(self):
        """Registers the node with the C4D server."""
        task_id = os.getenv("TASK_ID", "unknown")
        namespace = os.getenv("NAMESPACE", "unknown")
        payload = {
            # "node_id": os.getenv("NODE_ID", "unknown"),
            "node_id": task_id,
            "node_url": f"http://{task_id}.{namespace}:8081",
            "node_status": self.node_status,
        }
        try:
            response = requests.post(f"{self.c4d_server_url}/register", json=payload, timeout=10)
            if response.status_code == 200:
                print(f"Node {payload['node_id']} successfully registered with C4D server.")
            else:
                print(f"Failed to register node with C4D server. Status code: {response.status_code}")
        except requests.RequestException as e:
            print(f"Error registering node with C4D server: {e}")

    def append_ccl_log(self, log_data):
        with self.ccl_lock:
            self.ccl_logs.append(log_data)

    def get_ccl_logs(self):
        with self.ccl_lock:
            return self.ccl_logs.copy()

    def clear_ccl_logs(self):
        with self.ccl_lock:
            self.ccl_logs.clear()

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


def get_node_pid(node_id):
    """Fetches the PID of the process running the node with the given node_id."""
    try:
        response = requests.get(f"http://{node_id}:8081/pid", timeout=5)
        if response.status_code == 200:
            return int(response.json().get("pid"))
        else:
            print(f"Failed to fetch PID from node {node_id}. Status code: {response.status_code}")
            return None
    except requests.RequestException as e:
        print(f"Error fetching PID from node {node_id}: {e}")
        return None


# Create a Monitor instance
monitor = Monitor()
monitor.register_with_server()
if not monitor.is_standby:
    monitor.start()


@app.route('/metrics', methods=['GET'])
def get_metrics():
    return jsonify({
        "ram_usage": monitor.ram_usage,
        "cpu_usage": monitor.cpu_usage,
        "compute_engine_status": monitor.compute_engine_status,
        # TODO latency
    })


@app.route('/env', methods=['GET'])
def get_env():
    return jsonify(
        {key: value for key, value in os.environ.items()}
    )


@app.route('/log_ccl', methods=['GET', 'POST'])
def log_ccl():
    # Get data from URL parameters
    data = request.json
    if data["opType"] in ["send", "recv"]:
        # json.field("opType", opType)
        # .field("remoteRank", remoteRank)
        # .field("context_rank", rank_)
        # .field("context_size", size_)
        # .field("bytes", bytes)
        # .field("startTime", std::chrono::duration_cast<std::chrono::microseconds>(startTime.time_since_epoch()).count())
        # .field("endTime", std::chrono::duration_cast<std::chrono::microseconds>(endTime.time_since_epoch()).count())
        # .field("filename", filename)
        # .field("finished", finished);
        data["remote_rank"] = int(data["remoteRank"])
        data["context_rank"] = int(data["context_rank"])
        data["context_size"] = int(data["context_size"])
        data["bytes"] = int(data["bytes"])
        data["startTime"] = int(data["startTime"])
        data["endTime"] = int(data["endTime"])
        data["finished"] = bool(data["finished"])
    else:
        # json.field("opType", opType)
        # .field("algorithm", algorithm)
        # .field("dataType", dataType)
        # .field("count", count)
        # .field("rootRank", rootRank)
        # .field("context_rank", rank_)
        # .field("context_size", size_)
        # .field("startTime", std::chrono::duration_cast < std::chrono::microseconds > (
        #     startTime.time_since_epoch()).count())
        # .field("endTime", std::chrono::duration_cast < std::chrono::microseconds > (endTime.time_since_epoch()).count())
        # .field("filename", filename)
        # .field("finished", finished);
        data["count"] = int(data["count"])
        data["root_rank"] = int(data["rootRank"])
        data["context_rank"] = int(data["context_rank"])
        data["context_size"] = int(data["context_size"])
        data["startTime"] = int(data["startTime"])
        data["endTime"] = int(data["endTime"])
        data["finished"] = bool(data["finished"])
    # Remove None values, warn if found
    data = {k: v for k, v in data.items() if v is not None}
    if len(data) != len(request.json):
        print("Warning: Some data was missing from the request.")

    # Add timestamp
    data["timestamp"] = time.time()

    # Append to CCL logs
    monitor.append_ccl_log(data)

    return "OK", 200


@app.route('/dist_env', methods=['GET'])
def get_dist_env():
    return jsonify(monitor.dist_env_vars)


@app.route('/log_training_time', methods=['POST'])
def log_training_time():
    data = request.get_json()
    training_time = data.get("iteration_time")
    iteration = data.get("iteration")
    if training_time is not None:
        monitor.append_metric("training_time", training_time)
        return jsonify({"message": "Training time logged", "iteration": iteration}), 200
    return jsonify({"error": "Invalid data"}), 400


@app.route('/offload', methods=['POST'])
def offload_task():
    data = request.get_json()
    target_node_id = data.get("target_node_id")
    checkpoint_path = data.get("checkpoint_path")

    if not target_node_id or not checkpoint_path:
        return jsonify({"error": "Invalid offload request"}), 400

    # Notify the target node by sending a signal
    try:
        node_pid = get_node_pid(target_node_id)  # Implement a function to fetch node's PID
        os.kill(node_pid, signal.SIGUSR2)  # Send SIGUSR2 to the target node
        return jsonify({"message": f"Offload signal sent to node {target_node_id}"}), 200
    except Exception as e:
        return jsonify({"error": str(e)}), 500


@app.route('/activate', methods=['POST'])
def activate_node():
    monitor.is_standby = False
    monitor.node_status = "active"
    monitor.register
    monitor.start()  # Start sending metrics and running training code
    return jsonify({"message": "Node activated and ready to participate."}), 200


@app.route('/remove', methods=['POST'])
def remove_node():
    monitor.stop()  # Stop sending metrics and participating in training
    # monitor.is_standby = True
    # monitor.node_status = "standby"
    return jsonify({"message": "Node removed from training."}), 200


@app.route('/pid', methods=['GET'])
def get_pid():
    return jsonify({"pid": os.getpid()})


def shutdown_handler(signum, frame):
    print("Shutting down monitor...")
    monitor.stop()
    exit(0)


signal.signal(signal.SIGTERM, shutdown_handler)

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8081)