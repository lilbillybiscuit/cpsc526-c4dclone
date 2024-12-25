FROM rust:1.83-bookworm AS builder

WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src ./src

RUN cargo build --release --target-dir /tmp/build && \
    cp /tmp/build/release/compute-engine /usr/bin/compute-engine


FROM lilbillybiscuit/pytorch-c4d:latest
COPY --from=builder /usr/bin/compute-engine /usr/bin/compute-engine

WORKDIR /app
COPY user_code .

RUN pip install -r requirements.txt
EXPOSE 8080

CMD ["/usr/bin/compute-engine"]