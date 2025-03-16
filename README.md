# Flow Generator

![Build and push Docker image](https://github.com/philipschmid/flow-generator-app/actions/workflows/build.yaml/badge.svg) ![Go syntax and format check](https://github.com/philipschmid/flow-generator-app/actions/workflows/lint.yaml/badge.svg) ![Go tests and app build](https://github.com/philipschmid/flow-generator-app/actions/workflows/test.yaml/badge.svg)

This project provides a server and client to generate network flows (TCP and UDP) for Kubernetes network testing purposes (e.g., for Cilium and Hubble). The server echoes back received data, while the client generates configurable flows to simulate network traffic.

## Setup

### Build the Binaries
Compile the server and client binaries locally:
```bash
make build
```

### Run the Server
Start the echo server with debug logging enabled:
```bash
./echo-server --log_level=debug
```

### Run the Client
Generate flows to a server with custom settings:
```bash
./flow-generator --rate=10 --max_concurrent=50 --protocol=both --min_duration=1 --max_duration=5 --log_level=info
```

## Configuration Options

Server (`echo-server`/`ghcr.io/philipschmid/flow-generator:main`):
* `--log_level`: Log level (default: info)
* `--log_format`: Log format: human or json (default: human)
* `--metrics_port`: Port for Prometheus metrics (default: 9090)
* `--tracing_enabled`: Enable OpenTelemetry tracing (default: false)
* `--jaeger_endpoint`: Jaeger collector endpoint (default: "http://localhost:14268/api/traces")

Client (`flow-generator`/`ghcr.io/philipschmid/echo-server:main`):
* `--server`: Server address (default: "localhost")
* `--rate`: Flows per second (default: 10)
* `--max_concurrent`: Max concurrent flows (default: 100)
* `--protocol`: Protocol (tcp, udp, both; default: both)
* `--min_duration`: Min flow duration in seconds (default: 1.0)
* `--max_duration`: Max flow duration in seconds (default: 10.0)
* `--payload_size`: Fixed payload size in bytes (overrides `min_payload_size`/`max_payload_size`)
* `--min_payload_size`: Minimum payload size in bytes for dynamic range
* `--max_payload_size`: Maximum payload size in bytes for dynamic range
* `--mtu`: Maximum Transmission Unit in bytes
* `--mss`: Maximum Segment Size in bytes
* `--log_level`: Log level (default: info)
* `--log_format`: Log format: human or json (default: human)
* `--tracing_enabled`: Enable OpenTelemetry tracing (default: false)
* `--jaeger_endpoint`: Jaeger collector endpoint (default: "http://localhost:14268/api/traces")

## Example Usages

1. Basic Server and Client:

```bash
# Start the server:
./echo-server --log_level=info
# Run the client against it:
./flow-generator --rate=5
# Run the client against it (on Kubernetes):
./flow-generator --server=echo-service --rate=5
```

1. Advanced Client with High Concurrency:
```bash
./flow-generator --rate=20 --max_concurrent=200 --protocol=both --log_level=debug
```

1. Kubernetes Deployment: Deploy the server and client in Kubernetes:
```bash
kubectl apply -f k8s/server.yaml
kubectl apply -f k8s/client.yaml
```