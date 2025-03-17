# Flow Generator

![Build and push Docker image](https://github.com/philipschmid/flow-generator-app/actions/workflows/build.yaml/badge.svg) ![Go syntax and format check](https://github.com/philipschmid/flow-generator-app/actions/workflows/lint.yaml/badge.svg) ![Go tests and app build](https://github.com/philipschmid/flow-generator-app/actions/workflows/test.yaml/badge.svg)

This project provides a server and client to generate network flows (TCP and UDP) for Kubernetes network testing (e.g., for Cilium and Hubble). The server echoes back received data, while the client generates configurable flows to simulate network traffic.

## Setup

### Build the Binaries
Compile the server and client binaries locally:
```bash
make build
```

### Run the Server
Start the echo server with custom ports:
```bash
./echo-server --tcp_ports=8080,8443 --udp_ports=5353 --log_level=debug
```

### Run the Client
Generate flows with custom settings:
```bash
./flow-generator --tcp_ports=8080,8443 --udp_ports=5353 --rate=10 --max_concurrent=50 --protocol=both --log_level=debug
```

## Configuration Options

Server (`echo-server`/`ghcr.io/philipschmid/flow-generator:main`):
* `--log_level`: Log level (default: `info`)
* `--log_format`: Log format: `human` or `json` (default: `human`)
* `--metrics_port`: Port for Prometheus metrics (default: `9090`)
* `--tracing_enabled`: Enable OpenTelemetry tracing (default: `false`)
* `--jaeger_endpoint`: Jaeger collector endpoint (default: `http://localhost:14268/api/traces`)
* `--tcp_ports`: Comma-separated list of TCP ports (default: `8080`)
* `--udp_ports`: Comma-separated list of UDP ports (default: `""`)

Client (`flow-generator`/`ghcr.io/philipschmid/echo-server:main`):
* `--server`: Server address (default: `localhost`)
* `--rate`: Flows per second (default: `10`)
* `--max_concurrent`: Max concurrent flows (default: `100`)
* `--protocol`: Protocol (`tcp`, `udp`, `both`; default: `both`)
* `--min_duration`: Min flow duration in seconds (default: `1.0`)
* `--max_duration`: Max flow duration in seconds (default: `10.0`)
* `--constant_flows`: Enable constant flow mode (disables randomization; default: `false`)
* `--tcp_ports`: Comma-separated list of TCP ports (default: `8080`)
* `--udp_ports`: Comma-separated list of UDP ports (default: `""`)
* `--payload_size`: Fixed payload size in bytes (overrides `min_payload_size`/`max_payload_size`)
* `--min_payload_size`: Minimum payload size in bytes for dynamic range
* `--max_payload_size`: Maximum payload size in bytes for dynamic range
* `--mtu`: Maximum Transmission Unit in bytes (default: `1500`)
* `--mss`: Maximum Segment Size in bytes (default: `1460`)
* `--log_level`: Log level (default: `info`)
* `--log_format`: Log format: `human` or `json` (default: `human`)
* `--tracing_enabled`: Enable OpenTelemetry tracing (default: `false`)
* `--jaeger_endpoint`: Jaeger collector endpoint (default: `http://localhost:14268/api/traces`)

## Example Use-Cases

### Constant Flows

Simulate 5 TCP flows per second on port 8080:

```bash
./echo-server --tcp_ports=8080
./flow-generator --server=localhost --tcp_ports=8080 --rate=5 --max_concurrent=50 --constant_flows=true
```

or for Kubernetes:

```bash
kubectl apply -f k8s/server-constant.yaml
kubectl apply -f k8s/client-constant.yaml
```

This generates exactly 5 flows per second on TCP port 8080. Each flow lasts 50 / 5 = 10 seconds, ensuring a steady state of 50 concurrent flows, achieving the target rate without randomization.

### Pseudo-Random Traffic

Simulate random traffic across multiple ports:

```bash
./echo-server --tcp_ports=8080,8443 --udp_ports=53,123
./flow-generator --server=localhost --tcp_ports=8080,8443 --udp_ports=53,123 --rate=20 --max_concurrent=200 --protocol=both --min_duration=1 --max_duration=5
```

or for Kubernetes:

```bash
kubectl apply -f k8s/server-random.yaml
kubectl apply -f k8s/client-random.yaml
```

This generates an average of 20 flows per second across multiple ports, with durations randomized between `min_duration` and `max_duration`, simulating realistic Kubernetes traffic.

## Known Limitations

### Deep Packet Inspection (DPI) and Protocol Simulation
The `flow-generator-app` simulates Layer 7 (L7) protocols by utilizing well-known ports, such as port 80 for HTTP or port 53 for DNS. However, it does not implement actual L7 protocol logic. Instead, the server simply echoes back any data it receives on these ports without adhering to specific protocol formats. This design choice leads to the following limitation:

* Impact on DPI Tools: Deep Packet Inspection (DPI) tools analyze packet payloads to classify traffic based on L7 protocols. These tools depend on protocol-specific patterns (e.g., HTTP headers for HTTP traffic) to accurately identify the traffic type. Since the payloads in this application lack proper formatting, DPI tools may fail to recognize the traffic as the intended protocol, potentially classifying it as "Unknown" or misidentifying it entirely.
* Effect on Network Policies: Network policies that rely on L7 protocol detection for enforcement (e.g., allowing or blocking HTTP traffic) may not work as expected due to this misclassification.