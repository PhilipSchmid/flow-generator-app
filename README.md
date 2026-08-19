# Flow Generator

![Build and push Docker image](https://github.com/philipschmid/flow-generator-app/actions/workflows/build.yaml/badge.svg) ![CI](https://github.com/philipschmid/flow-generator-app/actions/workflows/ci.yaml/badge.svg)

Flow Generator runs a TCP/UDP echo server and a traffic client for testing Kubernetes networks, including Cilium and Hubble. The client starts flows at a controlled rate; the server echoes their payloads.

## Why This Exists

[iperf3](https://software.es.net/iperf/) measures the maximum achievable bandwidth of TCP, UDP, or SCTP paths and reports results such as throughput and loss. Flow Generator tests a different workload: many TCP/UDP flows starting, overlapping, and ending across configurable ports. Use iperf3 to measure path capacity. Use Flow Generator to exercise connection churn, network policy, and observability while controlling start rate, duration, concurrency, and payload size.

## Features

- **TCP and UDP**: Generate either protocol or mix both
- **Configuration**: Use command-line flags, `FLOW_GENERATOR_` environment variables, or optional config files
- **Observability**: Prometheus metrics, server health checks, and optional OpenTelemetry tracing
- **Terminal dashboard**: Inspect a running client or server without enabling verbose logs
- **Bounded concurrency**: Pace flow starts evenly and cap active flows
- **Kubernetes manifests**: Run constant or random traffic patterns
- **Development tools**: Live reload, tests, benchmarks, and CI workflows

## Quick Start

### Run Prebuilt Docker Images

```bash
# Run the echo server
docker run -p 8080:8080 -p 8082:8082 -p 9090:9090 ghcr.io/philipschmid/echo-server:latest

# Run the flow generator and publish its metrics endpoint
docker run -p 9091:9091 ghcr.io/philipschmid/flow-generator:latest --server host.docker.internal
```

### Build from Source

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

Set any option through an environment variable with the `FLOW_GENERATOR_` prefix:

```bash
export FLOW_GENERATOR_LOG_LEVEL=debug
export FLOW_GENERATOR_METRICS_PORT=9091 # client default; the server defaults to 9090
```

Flags override environment variables, which override `config.{yaml,json,toml}` in the current directory, `/etc/flow-generator`, or `~/.flow-generator`. CLI flags use hyphens; the former underscore spellings remain accepted. Environment variables and config keys keep underscores.

### Server Configuration

Server options for `echo-server` and `ghcr.io/philipschmid/echo-server:latest`:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--version` | — | `false` | Print version information and exit |
| `--log-level` | `FLOW_GENERATOR_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--log-format` | `FLOW_GENERATOR_LOG_FORMAT` | `human` | Log format (human, json) |
| `--metrics-port` | `FLOW_GENERATOR_METRICS_PORT` | `9090` | Prometheus metrics port |
| `--status-port` | `FLOW_GENERATOR_STATUS_PORT` | `9190` | Loopback dashboard status port (`0` disables it) |
| `--health-port` | `FLOW_GENERATOR_HEALTH_PORT` | `8082` | Health check server port |
| `--tracing-enabled` | `FLOW_GENERATOR_TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `--jaeger-endpoint` | `FLOW_GENERATOR_JAEGER_ENDPOINT` | `http://localhost:4317` | OTLP/gRPC collector URL (`http` is plaintext; `https` uses TLS) |
| `--tcp-ports-server` | `FLOW_GENERATOR_TCP_PORTS_SERVER` | `8080` | Comma-separated TCP ports |
| `--udp-ports-server` | `FLOW_GENERATOR_UDP_PORTS_SERVER` | `""` | Comma-separated UDP ports |

### Client Configuration

Client options for `flow-generator` and `ghcr.io/philipschmid/flow-generator:latest`:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--version` | — | `false` | Print version information and exit |
| `--log-level` | `FLOW_GENERATOR_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--log-format` | `FLOW_GENERATOR_LOG_FORMAT` | `human` | Log format (human, json) |
| `--metrics-port` | `FLOW_GENERATOR_METRICS_PORT` | `9091` | Prometheus metrics port |
| `--status-port` | `FLOW_GENERATOR_STATUS_PORT` | `9191` | Loopback dashboard status port (`0` disables it) |
| `--tracing-enabled` | `FLOW_GENERATOR_TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `--jaeger-endpoint` | `FLOW_GENERATOR_JAEGER_ENDPOINT` | `http://localhost:4317` | OTLP/gRPC collector URL (`http` is plaintext; `https` uses TLS) |
| `--server` | `FLOW_GENERATOR_SERVER` | `localhost` | Target server address |
| `--rate` | `FLOW_GENERATOR_RATE` | `10` | Target flow starts per second |
| `--max-concurrent` | `FLOW_GENERATOR_MAX_CONCURRENT` | `100` | Maximum concurrent flows |
| `--protocol` | `FLOW_GENERATOR_PROTOCOL` | `both` | Protocol (tcp, udp, both) |
| `--tcp-ports` | `FLOW_GENERATOR_TCP_PORTS` | `8080` | Comma-separated TCP ports |
| `--udp-ports` | `FLOW_GENERATOR_UDP_PORTS` | `""` | Comma-separated UDP ports |
| `--min-duration` | `FLOW_GENERATOR_MIN_DURATION` | `1.0` | Minimum flow duration (seconds) |
| `--max-duration` | `FLOW_GENERATOR_MAX_DURATION` | `10.0` | Maximum flow duration (seconds) |
| `--constant-flows` | `FLOW_GENERATOR_CONSTANT_FLOWS` | `false` | Use a fixed duration of `max-concurrent / rate` instead of a random duration |
| `--flow-timeout` | `FLOW_GENERATOR_FLOW_TIMEOUT` | `0` | Total runtime limit in seconds (`0` = unlimited) |
| `--flow-count` | `FLOW_GENERATOR_FLOW_COUNT` | `0` | Number of flows to start (`0` = unlimited) |
| `--payload-size` | `FLOW_GENERATOR_PAYLOAD_SIZE` | `0` | Fixed payload size in bytes; overrides the range (`0` selects range or 5-byte fallback) |
| `--min-payload-size` | `FLOW_GENERATOR_MIN_PAYLOAD_SIZE` | `0` | Minimum random payload size; set with `max-payload-size` |
| `--max-payload-size` | `FLOW_GENERATOR_MAX_PAYLOAD_SIZE` | `0` | Maximum random payload size; set with `min-payload-size` |
| `--mtu` | `FLOW_GENERATOR_MTU` | `1500` | Maximum allowed UDP payload size in bytes |
| `--mss` | `FLOW_GENERATOR_MSS` | `1460` | TCP segmentation warning threshold in bytes |

## Usage Examples

### Basic TCP Echo Test

```bash
# Start server
./bin/echo-server --tcp-ports-server=8080

# Generate flows
./bin/flow-generator --server=localhost --tcp-ports=8080 --rate=10
```

### Multi-Port Mixed Protocol Test

```bash
# Start server with multiple ports
./bin/echo-server --tcp-ports-server=8080,8443 --udp-ports-server=53,123

# Generate mixed traffic
./bin/flow-generator \
  --server=localhost \
  --tcp-ports=8080,8443 \
  --udp-ports=53,123 \
  --protocol=both \
  --rate=20 \
  --max-concurrent=200
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

```bash
./bin/flow-generator \
  --server=localhost \
  --tcp-ports=8080 \
  --rate=5 \
  --max-concurrent=50 \
  --constant-flows=true
```

Starts are paced evenly at `1 / rate` intervals; there is no configurable ramp-up period. If `--max-concurrent` is already occupied, that scheduled start is dropped rather than queued. TCP sends and receives one echo per flow, while UDP performs up to one echo exchange per 100 ms per active flow. `--rate` is not a packet or bandwidth limit, so high UDP concurrency and payload sizes can saturate a link.

In constant mode, each flow lasts `max-concurrent / rate` seconds. The example therefore targets about 5 starts per second and about 50 concurrent flows after warm-up, but ticker timing, connection overhead, errors, and dropped starts make the rate and concurrency approximate rather than exact.

`--flow-count` stops after the requested number of flows have started and drains them to their individual durations. `--flow-timeout`, `SIGINT`, and `SIGTERM` cancel active flows promptly.

## Monitoring

### Terminal Dashboard

Both container images include `/dashboard`. It reads a loopback-only status endpoint from the running client or server, so it works in the existing scratch images without a shell:

```bash
# Kubernetes
kubectl exec -it deploy/echo-server -- /dashboard
kubectl exec -it deploy/flow-generator -- /dashboard

# Docker
docker exec -it echo-server /dashboard
docker exec -it flow-generator /dashboard
```

The heading shows the process role, version, uptime, sample age, health, and active configuration. The client view adds target attainment, concurrency headroom, flow outcomes, payload throughput, protocol and port activity, and sampled echo latency. The server view shows request activity, active TCP connections, and unique active TCP client IPs. UDP is connectionless, so UDP senders are represented by packet and port activity instead of a connected-client count.

The dashboard keeps 1-, 5-, and 15-minute averages visible. Charts label the selected time window and auto-scaled vertical range; the dashed flow-rate line marks the configured target, and the table provides exact percentiles. Monitor-style shortcuts are also shown in the footer:

| Key | Action |
|-----|--------|
| `←` / `→`, `h` / `l`, `Tab` / `Shift+Tab` | Change the chart and percentile window |
| `1`, `5`, `0` | Select the 1-, 5-, or 15-minute window |
| `r` | Refresh now |
| `?`, `F1` | Toggle dashboard help |
| `q`, `F10` | Leave the dashboard; the monitored process keeps running |

Latency sampling is capped at roughly 1,000 flows per second. TCP measures its echo; UDP measures the first successful echo in a sampled flow rather than timing every packet. The dashboard reports p50, p90, p95, and p99 when enough samples exist.

The dashboard auto-detects the default local endpoints. For a custom status port, pass the full loopback URL:

```bash
/dashboard --endpoint http://127.0.0.1:9291
```

Use `--color=auto`, `always`, or `never`; `auto` also honors `NO_COLOR`. The light/dark palette distinguishes TCP, UDP, transmit, receive, healthy, capacity-limited, and failed states without relying on color alone. The status listener accepts only local GET requests, exposes no credentials, and can be disabled with `--status-port=0`.

### Health Checks

The echo server exposes health endpoints on port 8082 by default. The client does not run a health server.

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

Key metrics:

- `active_tcp_connections`: Current active TCP connections
- `udp_packets_received_total`: Total UDP packets received
- `tcp_connections_opened_total`: TCP connections opened by the client or accepted by the server
- `requests_sent_total{protocol,port}` / `requests_received_total{protocol,port}`: Client sends and server receives per protocol/port
- `bytes_sent_total{protocol,port}` / `bytes_received_total{protocol,port}`: Byte counts per protocol/port

### OpenTelemetry Tracing

When tracing is enabled, both binaries export OTLP/gRPC spans to `http://localhost:4317` by default:

```bash
./bin/echo-server --tracing-enabled=true --jaeger-endpoint=http://localhost:4317
./bin/flow-generator --server=localhost --tracing-enabled=true --jaeger-endpoint=http://localhost:4317
```

The client creates `network.flow` spans; the server creates `tcp.echo` and `udp.echo` spans. Trace context is not propagated through the raw TCP/UDP payloads, so client and server spans are separate traces.

## Architecture

Both binaries use the private packages under `internal/`:

- **cmd/**: Application entry points (server and client)
- **internal/**: Private application code
  - **config/**: Configuration management with validation
  - **handlers/**: Protocol-specific request handlers
  - **server/**: Server implementations with manager pattern
  - **metrics/**: Prometheus metrics collection
  - **status/**: Loopback dashboard snapshots and sampled latency
  - **dashboard/**: Terminal rendering, history, and status client
  - **health/**: Health check server for liveness/readiness probes
  - **logging/**: Structured logging utilities
  - **tracing/**: OpenTelemetry integration
  - **version/**: Version information management

## Development

The repository includes:

- **Live reload**: run `make install-tools`, then use `make dev-server` or `make dev-client`
- **Cross-platform builds**: `make build-all`
- **Testing**: Unit tests, a client/server parameter matrix, benchmarks, and integration tests
- **CI/CD**: Automated testing, security scanning, and multi-platform Docker builds

See [DEVELOPMENT.md](DEVELOPMENT.md) for detailed development instructions.

## Known Limitations

### Deep Packet Inspection (DPI) and Protocol Simulation

The generator sends generic TCP or UDP echo traffic. It does not implement application protocols such as HTTP or DNS, and using a well-known port does not make the payload valid L7 traffic.

As a result, DPI tools may classify the traffic as unknown, and L7-aware network policies may not behave as they would with valid application traffic.

## Contributing

Read the [contribution guidelines](DEVELOPMENT.md#contributing) before opening a change.

## License

This project is licensed under the Apache License 2.0; see [LICENSE](LICENSE) for details.
