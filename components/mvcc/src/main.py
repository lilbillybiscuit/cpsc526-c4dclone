import threading
import time
from flask import Flask, request, jsonify

app = Flask(__name__)

class MVCCNode:
    def __init__(self):
        self.data = {}  # {version: {data}}
        self.current_version = 0
        self.lock = threading.Lock()

    def create_version(self, data):
        with self.lock:
            self.current_version += 1
            self.data[self.current_version] = data
            return self.current_version

    def get_version(self, version):
        with self.lock:
            if version in self.data:
                return self.data[version]
            else:
                return None

    def get_latest_version(self):
        with self.lock:
            return self.data.get(self.current_version)

mvcc = MVCCNode()

@app.route('/version', methods=['POST'])
def create_version():
    data = request.get_json()
    version = mvcc.create_version(data)
    return jsonify({"version": version})

@app.route('/version/<int:version>', methods=['GET'])
def get_version(version):
    data = mvcc.get_version(version)
    if data:
        return jsonify(data)
    else:
        return jsonify({"error": "Version not found"}), 404

@app.route('/latest', methods=['GET'])
def get_latest_version():
    data = mvcc.get_latest_version()
    if data:
        return jsonify(data)
    else:
        return jsonify({"error": "No versions found"}), 404

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8083)