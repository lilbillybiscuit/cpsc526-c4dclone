import requests
from flask import Flask, request, jsonify

app = Flask(__name__)

# Dictionary to store registered failure agents
# Key: pod_name, Value: agent_url
failure_agents = {}

@app.route('/register', methods=['POST'])
def register():
    """Registers a failure agent."""
    data = request.get_json()
    pod_name = data.get("pod_name")
    agent_url = data.get("agent_url")
    
    if not pod_name or not agent_url:
        return jsonify({"error": "Missing pod_name or agent_url"}), 400

    failure_agents[pod_name] = agent_url
    print(f"Failure agent registered: {pod_name} at {agent_url}")
    return jsonify({"message": "Failure agent registered", "pod_name": pod_name})

@app.route('/inject_failure', methods=['POST'])
def inject_failure():
    """Injects a failure in a specified pod by sending a request to the corresponding failure agent."""
    data = request.get_json()
    pod_name = data.get("pod_name")

    if not pod_name:
        return jsonify({"error": "Missing pod_name"}), 400

    agent_url = failure_agents.get(pod_name)
    if not agent_url:
        return jsonify({"error": f"No failure agent found for pod: {pod_name}"}), 404

    try:
        response = requests.post(f"{agent_url}/inject_failure", timeout=5)
        response.raise_for_status()  # Raise an exception for bad status codes
        return jsonify({"message": f"Failure injection initiated in pod: {pod_name}"})
    except requests.exceptions.RequestException as e:
        return jsonify({"error": f"Error injecting failure in pod {pod_name}: {e}"}), 500

@app.route('/inject_latency', methods=['POST'])
def inject_latency():
    """Injects network latency in a specified pod."""
    data = request.get_json()
    pod_name = data.get("pod_name")
    latency_ms = data.get("latency_ms")
    interface = data.get("interface", "eth0")  # Default to eth0 if not specified

    if not pod_name or latency_ms is None:
        return jsonify({"error": "Missing pod_name or latency_ms"}), 400

    agent_url = failure_agents.get(pod_name)
    if not agent_url:
        return jsonify({"error": f"No failure agent found for pod: {pod_name}"}), 404

    try:
        response = requests.post(f"{agent_url}/inject_latency", json={"latency_ms": latency_ms, "interface": interface}, timeout=5)
        response.raise_for_status()
        return jsonify({"message": f"Latency of {latency_ms}ms injected in pod: {pod_name} on interface: {interface}"})
    except requests.exceptions.RequestException as e:
        return jsonify({"error": f"Error injecting latency in pod {pod_name}: {e}"}), 500

@app.route('/reset_network', methods=['POST'])
def reset_network():
    """Resets network conditions in a specified pod."""
    data = request.get_json()
    pod_name = data.get("pod_name")
    interface = data.get("interface", "eth0")  # Default to eth0 if not specified

    if not pod_name:
        return jsonify({"error": "Missing pod_name"}), 400

    agent_url = failure_agents.get(pod_name)
    if not agent_url:
        return jsonify({"error": f"No failure agent found for pod: {pod_name}"}), 404

    try:
        response = requests.post(f"{agent_url}/reset_network", json={"interface": interface}, timeout=5)
        response.raise_for_status()
        return jsonify({"message": f"Network conditions reset in pod: {pod_name} on interface: {