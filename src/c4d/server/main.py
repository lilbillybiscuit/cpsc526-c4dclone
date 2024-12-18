import os
from flask import Flask, request

app = Flask(__name__)

@app.route('/status')
def get_status():
    return {'status': 'running'}

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=8001)