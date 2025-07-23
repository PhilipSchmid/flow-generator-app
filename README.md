# Flow Generator

![Build and push Docker image](https://github.com/philipschmid/flow-generator-app/actions/workflows/build.yaml/badge.svg) ![CI](https://github.com/philipschmid/flow-generator-app/actions/workflows/ci.yaml/badge.svg)

This project provides a server and client to generate network flows (TCP and UDP) for Kubernetes network testing (e.g., for Cilium and Hubble). The server echoes back received data, while the client generates configurable flows to simulate network traffic.

## Features

- **Multi-protocol support**: TCP and UDP traffic generation
- **Flexible configuration**: Extensive command-line flags and environment variables
- **Production-ready**: Built-in Prometheus metrics and OpenTelemetry tracing
- **High performance**: Concurrent flow handling with configurable limits
- **Kubernetes-native**: Ready-to-use manifests for deployment
- **Developer-friendly**: Live reload, comprehensive testing, and CI/CD pipelines

## Quick Start

### Using Pre-built Docker Images

```bash
# Run the echo server
docker run -p 8080:8080 -p 8082:8082 -p 9090:9090 ghcr.io/philipschmid/echo-server:latest

# Run the flow generator
docker run ghcr.io/philipschmid/flow-generator:latest --server host.docker.internal
```

### Building from Source

```bash
# Clone the repository
git clone https://github.com/PhilipSchmid/flow-generator-app.git
cd flow-generator-app

# Build binaries
make build

# Run quick test
make quick-test
```

For detailed development instructions, see [DEVELOPMENT.md](DEVELOPMENT.md).

## Configuration

### Environment Variables

All configuration options can be set via environment variables with the `FLOW_GENERATOR_` prefix:

```bash
export FLOW_GENERATOR_LOG_LEVEL=debug
export FLOW_GENERATOR_METRICS_PORT=9090
```

### Server Configuration

The echo server (`echo-server` / `ghcr.io/philipschmid/echo-server:latest`) accepts the following options:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--log_level` | `FLOW_GENERATOR_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--log_format` | `FLOW_GENERATOR_LOG_FORMAT` | `human` | Log format (human, json) |
| `--metrics_port` | `FLOW_GENERATOR_METRICS_PORT` | `9090` | Prometheus metrics port |
| `--health_port` | `FLOW_GENERATOR_HEALTH_PORT` | `8082` | Health check server port |
| `--tracing_enabled` | `FLOW_GENERATOR_TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `--jaeger_endpoint` | `FLOW_GENERATOR_JAEGER_ENDPOINT` | `http://localhost:14268/api/traces` | Jaeger collector endpoint |
| `--tcp_ports_server` | `FLOW_GENERATOR_TCP_PORTS_SERVER` | `8080` | Comma-separated TCP ports |
| `--udp_ports_server` | `FLOW_GENERATOR_UDP_PORTS_SERVER` | `""` | Comma-separated UDP ports |

### Client Configuration

The flow generator (`flow-generator` / `ghcr.io/philipschmid/flow-generator:latest`) accepts the following options:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--server` | `FLOW_GENERATOR_SERVER` | `localhost` | Target server address |
| `--rate` | `FLOW_GENERATOR_RATE` | `10` | Flows per second |
| `--max_concurrent` | `FLOW_GENERATOR_MAX_CONCURRENT` | `100` | Maximum concurrent flows |
| `--protocol` | `FLOW_GENERATOR_PROTOCOL` | `both` | Protocol (tcp, udp, both) |
| `--tcp_ports` | `FLOW_GENERATOR_TCP_PORTS` | `8080` | Comma-separated TCP ports |
| `--udp_ports` | `FLOW_GENERATOR_UDP_PORTS` | `""` | Comma-separated UDP ports |
| `--min_duration` | `FLOW_GENERATOR_MIN_DURATION` | `1.0` | Minimum flow duration (seconds) |
| `--max_duration` | `FLOW_GENERATOR_MAX_DURATION` | `10.0` | Maximum flow duration (seconds) |
| `--constant_flows` | `FLOW_GENERATOR_CONSTANT_FLOWS` | `false` | Disable flow randomization |
| `--flow_timeout` | `FLOW_GENERATOR_FLOW_TIMEOUT` | `0` | Total runtime limit (0 = unlimited) |
| `--flow_count` | `FLOW_GENERATOR_FLOW_COUNT` | `0` | Maximum flows to generate (0 = unlimited) |
| `--payload_size` | `FLOW_GENERATOR_PAYLOAD_SIZE` | `0` | Fixed payload size (bytes) |
| `--min_payload_size` | `FLOW_GENERATOR_MIN_PAYLOAD_SIZE` | `0` | Minimum payload size (bytes) |
| `--max_payload_size` | `FLOW_GENERATOR_MAX_PAYLOAD_SIZE` | `0` | Maximum payload size (bytes) |
| `--mtu` | `FLOW_GENERATOR_MTU` | `1500` | Maximum Transmission Unit |
| `--mss` | `FLOW_GENERATOR_MSS` | `1460` | Maximum Segment Size |

Additional options for both server and client:
- `--log_level`, `--log_format`: Logging configuration
- `--tracing_enabled`, `--jaeger_endpoint`: Tracing configuration

## Usage Examples

### Basic TCP Echo Test

```bash
# Start server
./bin/echo-server --tcp_ports_server=8080

# Generate flows
./bin/flow-generator --server=localhost --tcp_ports=8080 --rate=10
```

### Multi-Port Mixed Protocol Test

```bash
# Start server with multiple ports
./bin/echo-server --tcp_ports_server=8080,8443 --udp_ports_server=53,123

# Generate mixed traffic
./bin/flow-generator \
  --server=localhost \
  --tcp_ports=8080,8443 \
  --udp_ports=53,123 \
  --protocol=both \
  --rate=20 \
  --max_concurrent=200
```

### Kubernetes Deployment

Deploy the pre-configured examples:

```bash
# Constant flow pattern
kubectl apply -f k8s/server-constant.yaml
kubectl apply -f k8s/client-constant.yaml

# Random flow pattern
kubectl apply -f k8s/server-random.yaml
kubectl apply -f k8s/client-random.yaml
```

### Constant Flow Mode

For predictable traffic patterns:

```bash
./bin/flow-generator \
  --server=localhost \
  --tcp_ports=8080 \
  --rate=5 \
  --max_concurrent=50 \
  --constant_flows=true
```

This generates exactly 5 flows per second, each lasting 10 seconds (50/5), maintaining a steady state of 50 concurrent flows.

## Monitoring

### Health Checks

The echo server exposes health check endpoints on a dedicated port (default: 8082):

```bash
# Liveness probe - basic health check
curl http://localhost:8082/health

# Readiness probe - indicates service is ready to accept traffic
curl http://localhost:8082/ready
```

### Prometheus Metrics

Both server and client expose Prometheus metrics on the configured port (default: 9090):

```bash
curl http://localhost:9090/metrics
```

Key metrics include:
- `tcp_connections_active`: Current active TCP connections
- `udp_packets_received_total`: Total UDP packets received
- `flows_generated_total`: Total flows generated by client
- Request/response counts and bytes per protocol/port

### OpenTelemetry Tracing

Enable distributed tracing:

```bash
./bin/echo-server --tracing_enabled=true --jaeger_endpoint=http://jaeger:14268/api/traces
```

## Architecture

The project follows a clean architecture pattern:

- **cmd/**: Application entry points (server and client)
- **internal/**: Private application code
  - **config/**: Configuration management with validation
  - **handlers/**: Protocol-specific request handlers
  - **server/**: Server implementations with manager pattern
  - **metrics/**: Prometheus metrics collection
  - **health/**: Health check server for liveness/readiness probes
  - **logging/**: Structured logging utilities
  - **tracing/**: OpenTelemetry integration
  - **version/**: Version information management

## Development

This project includes comprehensive development tools:

- **Live reload**: `make dev` for rapid development
- **Cross-platform builds**: `make build-all`
- **Testing**: Unit tests, benchmarks, and integration tests
- **CI/CD**: Automated testing, security scanning, and multi-platform Docker builds

See [DEVELOPMENT.md](DEVELOPMENT.md) for detailed development instructions.

## Known Limitations

### Deep Packet Inspection (DPI) and Protocol Simulation

The flow-generator-app simulates Layer 7 (L7) protocols by utilizing well-known ports (e.g., port 80 for HTTP, port 53 for DNS). However, it does not implement actual L7 protocol logic. The server simply echoes back any data it receives without adhering to specific protocol formats.

**Impact:**
- **DPI Tools**: May fail to recognize traffic as the intended protocol, potentially classifying it as "Unknown"
- **Network Policies**: L7-aware policies may not work as expected due to the lack of proper protocol formatting

## Contributing

Contributions are welcome! Please see [DEVELOPMENT.md](DEVELOPMENT.md#contributing) for guidelines.

## License

This project is licensed under the MIT License - see the LICENSE file for details.