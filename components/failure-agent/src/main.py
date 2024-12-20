import os
import sys
import time
import signal
import subprocess
import requests
from flask import Flask, request, jsonify

app = Flask(__name__)

# Configuration
COMPUTE_ENGINE_PID_FILE = "/tmp/compute_engine.pid"
FAILURE_SERVER_URL = os.environ.get("FAILURE_SERVER_URL", "http://failure-server:8082")

def get_compute_engine_pid():
    """Gets the PID of the compute engine process (you might need a different way to track this)."""
    try:
        with open(COMPUTE_ENGINE_PID_FILE, "r") as f:
            return int(f.read().strip())
    except FileNotFoundError:
        return None

def inject_failure():
    """Injects a failure by killing the compute engine process."""
    compute_engine_pid = get_compute_engine_pid()
    if compute_engine_pid:
        try:
            os.kill(compute_engine_pid, signal.SIGKILL)
            print(f"Injected failure: Killed compute engine process (PID: {compute_engine_pid})")
        except ProcessLookupError:
            print(f"Failed to kill process {compute_engine_pid}")
    else:
        print("Compute engine process not found. Cannot inject failure.")

def inject_network_latency(latency_ms, interface="eth0"):
    """Injects network latency using tc."""
    try:
        # Add latency using netem
        subprocess.run([
            "tc", "qdisc", "add", "dev", interface, "root", "netem", "delay", f"{latency_ms}ms"
        ], check=True)
        print(f"Injected {latency_ms}ms latency on {interface}")
    except subprocess.CalledProcessError as e:
        print(f"Error injecting latency: {e}")

def reset_network_conditions(interface="eth0"):
    """Resets network conditions using tc."""
    try:
        # Delete any existing qdisc (resetting conditions)
        subprocess.run([
            "tc", "qdisc", "del", "dev", interface, "root"
        ], check=True)
        print(f"Reset network conditions on {interface}")
    except subprocess.CalledProcessError as e:
        print(f"Error resetting network conditions: {e}")

def register_with_failure_server():
    """Registers the failure agent with the failure server."""
    pod_name = os.environ.get("HOSTNAME", "unknown")
    registration_data = {"pod_name": pod_name, "agent_url": f"http://{pod_name}:8082"}
    while True:
        try:
            response = requests.post(f"{FAILURE_SERVER_URL}/register", json=registration_data, timeout=5)
            if response.status_code == 200:
                print(f"Successfully registered with failure server: {response.json()}")
                break
            else:
                print(f"Failed to register with failure server. Status code: {response.status_code}")
        except requests.exceptions.ConnectionError as e:
            print(f"Failed to connect to failure server: {e}")
        time.sleep(5)

@app.route('/inject_failure', methods=['POST'])
def handle_inject_failure():
    """API endpoint to trigger failure injection."""
    inject_failure()
    return jsonify({"message": "Failure injection initiated."})

@app.route('/inject_latency', methods=['POST'])
def handle_inject_latency():
    """API endpoint to inject network latency."""
    data = request.get_json()
    latency_ms = data.get("latency_ms", 0)
    interface = data.get("interface", "eth0")
    inject_network_latency(latency_ms, interface)
    return jsonify({"message": f"Injected {latency_ms}ms latency on {interface}."})

@app.route('/reset_network', methods=['POST'])
def handle_reset_network():
    """API endpoint to reset network conditions."""
    interface = request.get_json().get("interface", "eth0")
    reset_network_conditions(interface)
    return jsonify({"message": f"Reset network conditions on {interface}."})

if __name__ == "__main__":
    register_with_failure_server()
    app.run(host="0.0.0.0", port=8082)