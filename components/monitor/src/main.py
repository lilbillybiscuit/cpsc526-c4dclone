import os
import psutil
import time
import threading
from flask import Flask, jsonify

app = Flask(__name__)

class Monitor:
    def __init__(self):
        self.stop_event = threading.Event()
        self.ram_usage = 0
        self.cpu_usage = 0
        self.compute_engine_status = "unknown"

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

    def start(self):
        """Starts the monitoring thread."""
        self.monitor_thread = threading.Thread(target=self.update_metrics)
        self.monitor_thread.start()
        print("Monitor started.")

    def stop(self):
        """Stops the monitoring thread."""
        self.stop_event.set()
        self.monitor_thread.join()
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

def shutdown_handler(signum, frame):
    print("Shutting down monitor...")
    monitor.stop()
    exit(0)

signal.signal(signal.SIGTERM, shutdown_handler)

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8081)