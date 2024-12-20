import socket
import subprocess

def check_port(ip, port):
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    result = sock.connect_ex((ip, port))
    sock.close()
    return result == 0

def main():
    master_ip = "master"  # Replace with your master node's IP
    port = 29500
    
    # Check if we can ping the master
    ping_result = subprocess.run(['ping', '-c', '1', master_ip], 
                               stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE)
    
    print(f"Ping test to {master_ip}: {'Success' if ping_result.returncode == 0 else 'Failed'}")
    
    # Check if the port is accessible
    port_accessible = check_port(master_ip, port)
    print(f"Port {port} on {master_ip} is {'open' if port_accessible else 'closed'}")

if __name__ == "__main__":
    main()
