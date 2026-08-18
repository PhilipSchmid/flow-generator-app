# Flow Generator

![Build and push Docker image](https://github.com/philipschmid/flow-generator-app/actions/workflows/build.yaml/badge.svg) ![CI](https://github.com/philipschmid/flow-generator-app/actions/workflows/ci.yaml/badge.svg)

This project provides a server and client to generate network flows (TCP and UDP) for Kubernetes network testing (e.g., for Cilium and Hubble). The server echoes back received data, while the client generates configurable flows to simulate network traffic.

## Features

- **Multi-protocol support**: TCP and UDP traffic generation
- **Flexible configuration**: Command-line flags, `FLOW_GENERATOR_` environment variables, and optional config files
- **Observable**: Prometheus metrics, server health checks, and optional OpenTelemetry tracing
- **Bounded concurrency**: Evenly paced flow starts with a configurable concurrency ceiling
- **Kubernetes-native**: Paired constant- and random-traffic manifests
- **Developer-friendly**: Live-reload targets, tests, benchmarks, and CI/CD workflows

## Quick Start

### Using Pre-built Docker Images

```bash
# Run the echo server
docker run -p 8080:8080 -p 8082:8082 -p 9090:9090 ghcr.io/philipschmid/echo-server:latest

# Run the flow generator and publish its metrics endpoint
docker run -p 9091:9091 ghcr.io/philipschmid/flow-generator:latest --server host.docker.internal
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
export FLOW_GENERATOR_METRICS_PORT=9091 # client default; the server defaults to 9090
```

Flags override environment variables, which override `config.{yaml,json,toml}` in the current directory, `/etc/flow-generator`, or `~/.flow-generator`.

### Server Configuration

The echo server (`echo-server` / `ghcr.io/philipschmid/echo-server:latest`) accepts the following options:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--version` | — | `false` | Print version information and exit |
| `--log_level` | `FLOW_GENERATOR_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--log_format` | `FLOW_GENERATOR_LOG_FORMAT` | `human` | Log format (human, json) |
| `--metrics_port` | `FLOW_GENERATOR_METRICS_PORT` | `9090` | Prometheus metrics port |
| `--health_port` | `FLOW_GENERATOR_HEALTH_PORT` | `8082` | Health check server port |
| `--tracing_enabled` | `FLOW_GENERATOR_TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `--jaeger_endpoint` | `FLOW_GENERATOR_JAEGER_ENDPOINT` | `http://localhost:4317` | OTLP/gRPC collector URL (`http` is plaintext; `https` uses TLS) |
| `--tcp_ports_server` | `FLOW_GENERATOR_TCP_PORTS_SERVER` | `8080` | Comma-separated TCP ports |
| `--udp_ports_server` | `FLOW_GENERATOR_UDP_PORTS_SERVER` | `""` | Comma-separated UDP ports |

### Client Configuration

The flow generator (`flow-generator` / `ghcr.io/philipschmid/flow-generator:latest`) accepts the following options:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--version` | — | `false` | Print version information and exit |
| `--log_level` | `FLOW_GENERATOR_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--log_format` | `FLOW_GENERATOR_LOG_FORMAT` | `human` | Log format (human, json) |
| `--metrics_port` | `FLOW_GENERATOR_METRICS_PORT` | `9091` | Prometheus metrics port |
| `--tracing_enabled` | `FLOW_GENERATOR_TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `--jaeger_endpoint` | `FLOW_GENERATOR_JAEGER_ENDPOINT` | `http://localhost:4317` | OTLP/gRPC collector URL (`http` is plaintext; `https` uses TLS) |
| `--server` | `FLOW_GENERATOR_SERVER` | `localhost` | Target server address |
| `--rate` | `FLOW_GENERATOR_RATE` | `10` | Target flow starts per second |
| `--max_concurrent` | `FLOW_GENERATOR_MAX_CONCURRENT` | `100` | Maximum concurrent flows |
| `--protocol` | `FLOW_GENERATOR_PROTOCOL` | `both` | Protocol (tcp, udp, both) |
| `--tcp_ports` | `FLOW_GENERATOR_TCP_PORTS` | `8080` | Comma-separated TCP ports |
| `--udp_ports` | `FLOW_GENERATOR_UDP_PORTS` | `""` | Comma-separated UDP ports |
| `--min_duration` | `FLOW_GENERATOR_MIN_DURATION` | `1.0` | Minimum flow duration (seconds) |
| `--max_duration` | `FLOW_GENERATOR_MAX_DURATION` | `10.0` | Maximum flow duration (seconds) |
| `--constant_flows` | `FLOW_GENERATOR_CONSTANT_FLOWS` | `false` | Use a fixed duration of `max_concurrent / rate` instead of a random duration |
| `--flow_timeout` | `FLOW_GENERATOR_FLOW_TIMEOUT` | `0` | Total runtime limit in seconds (`0` = unlimited) |
| `--flow_count` | `FLOW_GENERATOR_FLOW_COUNT` | `0` | Number of flows to start (`0` = unlimited) |
| `--payload_size` | `FLOW_GENERATOR_PAYLOAD_SIZE` | `0` | Fixed payload size in bytes; overrides the range (`0` selects range or 5-byte fallback) |
| `--min_payload_size` | `FLOW_GENERATOR_MIN_PAYLOAD_SIZE` | `0` | Minimum random payload size; set with `max_payload_size` |
| `--max_payload_size` | `FLOW_GENERATOR_MAX_PAYLOAD_SIZE` | `0` | Maximum random payload size; set with `min_payload_size` |
| `--mtu` | `FLOW_GENERATOR_MTU` | `1500` | Maximum allowed UDP payload size in bytes |
| `--mss` | `FLOW_GENERATOR_MSS` | `1460` | TCP segmentation warning threshold in bytes |

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

Choose one matching server/client pair; both pairs use the same Kubernetes resource names:

```bash
# Constant flow pattern
kubectl apply -f k8s/server-constant.yaml
kubectl apply -f k8s/client-constant.yaml

# Or replace it with the random flow pattern
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

Starts are paced evenly at `1 / rate` intervals; there is no configurable ramp-up period. If `max_concurrent` is already occupied, that scheduled start is dropped rather than queued. TCP sends and receives one echo per flow, while UDP performs up to one echo exchange per 100 ms per active flow. `rate` is not a packet or bandwidth limit, so high UDP concurrency and payload sizes can saturate a link.

In constant mode, each flow lasts `max_concurrent / rate` seconds. The example therefore targets about 5 starts per second and about 50 concurrent flows after warm-up, but ticker timing, connection overhead, errors, and dropped starts make the rate and concurrency approximate rather than exact.

`flow_count` stops after the requested number of flows have started and drains them to their individual durations. `flow_timeout`, `SIGINT`, and `SIGTERM` cancel active flows promptly.

## Monitoring

### Health Checks

The echo server exposes health endpoints on port 8082 by default. The client has no health server.

```bash
# Liveness probe - basic health check
curl http://localhost:8082/health

# Readiness probe - indicates service is ready to accept traffic
curl http://localhost:8082/ready
```

### Prometheus Metrics

Both binaries expose `/metrics`, with different defaults:

```bash
curl http://localhost:9090/metrics # server
curl http://localhost:9091/metrics # client
```

Metrics and health listeners bind to all interfaces and do not authenticate requests. Restrict access with container, host, or cluster network policy.

At info level, both binaries emit one aggregate progress heartbeat every 30 seconds. Per-flow diagnostics remain at debug level and repeated messages are sampled.

Key metrics include:
- `active_tcp_connections`: Current active TCP connections
- `udp_packets_received_total`: Total UDP packets received
- `tcp_connections_opened_total`: TCP connections opened by the client or accepted by the server
- `requests_sent_total{protocol,port}` / `requests_received_total{protocol,port}`: Client sends and server receives per protocol/port
- `bytes_sent_total{protocol,port}` / `bytes_received_total{protocol,port}`: Byte counts per protocol/port

### OpenTelemetry Tracing

When tracing is enabled, both binaries export OTLP/gRPC spans to `http://localhost:4317` by default:

```bash
./bin/echo-server --tracing_enabled=true --jaeger_endpoint=http://localhost:4317
./bin/flow-generator --server=localhost --tracing_enabled=true --jaeger_endpoint=http://localhost:4317
```

The client creates `network.flow` spans; the server creates `tcp.echo` and `udp.echo` spans. Trace context is not propagated through the raw TCP/UDP payloads, so client and server spans are separate traces.

## Architecture

The two binaries share private packages under `internal/`:

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

- **Live reload**: run `make install-tools`, then use `make dev-server` or `make dev-client`
- **Cross-platform builds**: `make build-all`
- **Testing**: Unit tests, benchmarks, and integration tests
- **CI/CD**: Automated testing, security scanning, and multi-platform Docker builds

See [DEVELOPMENT.md](DEVELOPMENT.md) for detailed development instructions.

## Known Limitations

### Deep Packet Inspection (DPI) and Protocol Simulation

The generator can send generic TCP or UDP echo traffic to well-known ports, but it does not implement application protocols such as HTTP or DNS. Port choice alone does not make the payload valid L7 traffic.

**Impact:**
- **DPI Tools**: May fail to recognize traffic as the intended protocol, potentially classifying it as "Unknown"
- **Network Policies**: L7-aware policies may not work as expected due to the lack of proper protocol formatting

## Contributing

Contributions are welcome! Please see [DEVELOPMENT.md](DEVELOPMENT.md#contributing) for guidelines.

## License

This project is licensed under the Apache License 2.0; see [LICENSE](LICENSE) for details.
