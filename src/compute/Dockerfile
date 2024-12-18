FROM ubuntu:22.04 as builder

RUN apt-get update && apt-get install -y \
    python3 \
    python3-pip \
    build-essential \
    git \
    cmake \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt .
RUN pip3 install --no-cache-dir -r requirements.txt

RUN pip3 install --no-cache-dir torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cpu

FROM ubuntu:22.04

COPY --from=builder /usr/local/lib/python3.10 /usr/local/lib/python3.10
COPY --from=builder /usr/local/bin /usr/local/bin

WORKDIR /app
COPY . .

CMD ["python3", "main.py"]