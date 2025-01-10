FROM golang:1.23 AS builder
WORKDIR /app

COPY go.mod ./
RUN go mod download
COPY . ./
COPY ./proto2 ./proto
RUN go build -o main .

FROM ubuntu:latest
WORKDIR /app
COPY --from=builder /app/main .
CMD ["./main"]