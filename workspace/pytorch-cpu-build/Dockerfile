FROM python:3.11-slim

# Install PyTorch and related packages for CPU only
RUN pip3 install --no-cache-dir torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cpu

# Set working directory
WORKDIR /app

# Command to run when container starts
CMD ["python3"]