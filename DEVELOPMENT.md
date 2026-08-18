# Development Guide

Run the commands in this guide from the repository root.

## Contents

- [Project Layout](#project-layout)
- [Setup](#setup)
- [Local Development](#local-development)
- [Building](#building)
- [Configuration](#configuration)
- [Testing](#testing)
- [Code Quality](#code-quality)
- [Docker](#docker)
- [Debugging](#debugging)
- [Performance Checks](#performance-checks)
- [Contributing](#contributing)
- [Releasing](#releasing)

## Project Layout

```text
flow-generator-app/
├── cmd/
│   ├── client/             # Flow generator
│   └── server/             # Echo server
├── internal/
│   ├── config/             # Configuration and validation
│   ├── handlers/           # TCP and UDP handlers
│   ├── health/             # Server health endpoints
│   ├── logging/            # Structured logging
│   ├── metrics/            # Prometheus and shutdown metrics
│   ├── server/             # TCP/UDP server lifecycle
│   ├── tracing/            # OpenTelemetry setup
│   └── version/            # Build metadata
├── test/                   # Binary-level integration tests
├── k8s/                    # Kubernetes manifests
├── scripts/                # Setup and scenario scripts
├── .github/workflows/      # CI and release automation
├── Dockerfile.client
├── Dockerfile.server
└── Makefile
```

## Setup

Requirements:

- Go 1.25 or newer
- Make
- Docker for container targets
- `curl` if `make install-tools` needs to install golangci-lint

Clone and prepare the repository:

```bash
git clone https://github.com/PhilipSchmid/flow-generator-app.git
cd flow-generator-app
make install-tools
make deps
make test
```

`make deps` downloads modules and runs `go mod tidy`, so it may update `go.mod` or `go.sum` when they are stale. `scripts/dev-setup.sh` runs the setup, build, and test steps and installs a local pre-commit hook.

Run `make help` for the current target list.

## Local Development

Start the server and client in separate terminals:

```bash
# Terminal 1
make dev-server

# Terminal 2
make dev-client
```

Both targets use [Air](https://github.com/air-verse/air) and rebuild on Go source changes. The client connects to `localhost` with its normal defaults.

For a finite mixed TCP/UDP smoke test:

```bash
make quick-test
```

The smoke test runs for 10 seconds and requires ports 8080, 8081, 8082, 9000, 9091, and 9092. Logs are written to `/tmp/echo-server.log` and `/tmp/flow-generator.log`.

## Building

```bash
# Current platform
make build

# One binary
make build-server
make build-client

# Linux, macOS, and Windows targets
make build-all
```

Current-platform binaries are written to `bin/`. Cross-platform binaries use `bin/<os>/<arch>/`; Windows outputs include `.exe`.

## Configuration

Configuration precedence is:

1. Command-line flags
2. `FLOW_GENERATOR_` environment variables
3. `config.yaml`, `config.json`, or `config.toml`
4. Built-in defaults

Config files are read from the current directory, `/etc/flow-generator`, or `~/.flow-generator`. CLI flags use hyphens, such as `--max-concurrent`. Existing underscore spellings such as `--max_concurrent` remain accepted. Environment variables and config keys keep their underscore form.

Examples:

```bash
export FLOW_GENERATOR_LOG_LEVEL=debug
export FLOW_GENERATOR_METRICS_PORT=9090

./bin/echo-server --log-level debug --metrics-port 9090
./bin/flow-generator --server localhost --tcp-ports 8080 --rate 10
```

See [README.md](README.md#configuration) for every option and default.

## Testing

```bash
# Unit and binary-level integration tests
make test

# Verbose output
make test-verbose

# Race detector
make test-race

# HTML coverage report at coverage/coverage.html
make test-coverage

# Local fixed-port TCP/UDP smoke test
make quick-test

# Seven local traffic scenarios
bash scripts/test-scenarios.sh

# Benchmarks with allocation counts
make benchmark
```

`make test` includes the tests under `test/`, which build both binaries and use dynamically assigned ports. `make quick-test` and `scripts/test-scenarios.sh` use fixed local ports; stop conflicting processes first.

## Code Quality

```bash
make fmt       # Rewrite Go files with gofmt
make vet       # Run go vet
make lint      # Run golangci-lint with the CI flags
make mod-tidy  # Run go mod tidy
```

Before committing a code change:

```bash
make fmt
make mod-tidy
git diff --check
make lint
make test-race
make build-all
```

Run `make quick-test` after network behavior changes and `make docker-build` after Dockerfile changes. CI also runs staticcheck, gosec, integration tests, and Linux container builds.

## Docker

```bash
# Build both images for the current Docker platform
make docker-build

# Build and start the echo server container
make docker-run

# Stop and remove that container
make docker-stop
```

`make docker-run` publishes TCP 8080, health 8082, and metrics 9090. It does not start the client. The release workflow builds both images for `linux/amd64` and `linux/arm64`.

## Debugging

Enable debug logs with `--log-level debug` or `FLOW_GENERATOR_LOG_LEVEL=debug`.

Default local endpoints:

- Server metrics: `http://localhost:9090/metrics`
- Client metrics: `http://localhost:9091/metrics`
- Server health: `http://localhost:8082/health`
- Server readiness: `http://localhost:8082/ready`

Common failures:

- **Address already in use**: stop the process holding the configured listener port.
- **Permission denied**: ports below 1024 may require extra privileges.
- **Connection refused**: confirm the server address, protocol, and port match the client.
- **Dropped starts**: raise `max-concurrent`, shorten flow duration, or lower `rate`.

## Performance Checks

Use finite runs while tuning:

```bash
# High flow churn without an immediate concurrency bottleneck
./bin/flow-generator \
  --server localhost \
  --rate 1000 \
  --max-concurrent 1000 \
  --min-duration 0.1 \
  --max-duration 0.5 \
  --flow-timeout 30

# Longer-lived flows
./bin/flow-generator \
  --server localhost \
  --rate 10 \
  --max-concurrent 2000 \
  --min-duration 60 \
  --max-duration 300 \
  --flow-timeout 300
```

`rate` controls flow starts, not packets or bandwidth. Starts are dropped when `max-concurrent` is full. Watch `starts_skipped_at_capacity` in the client progress log and use Prometheus metrics for traffic totals. `make benchmark` measures local CPU and allocation costs; it is not a network-capacity test.

## Contributing

1. Branch from `main`.
2. Keep each commit focused and add tests for behavior changes.
3. Commit with automatic sign-off: `git commit -s`.
4. Run the relevant checks above.
5. Open a pull request and wait for CI.

Use Conventional Commit subjects such as `feat:`, `fix:`, `docs:`, `test:`, `perf:`, or `chore:`. Pull requests are rebase-merged to keep history linear.

## Releasing

Tags drive releases. There is no version file or checked-in changelog to update.

1. Rebase-merge the intended changes and confirm `main` CI passes.
2. Create a signed SemVer tag on the release commit.
3. Push that tag.

```bash
git tag -s v1.2.3 -m "v1.2.3"
git push origin v1.2.3
```

The tag workflow builds and pushes both multi-platform images, generates SBOM artifacts, creates release notes from commit subjects, and publishes the GitHub release. It does not rerun the CI test suite, so tag only a commit that has already passed CI. Tags containing `-rc`, `-beta`, `-alpha`, or `-pre` produce prereleases.

## References

- [Go documentation](https://go.dev/doc/)
- [Prometheus practices](https://prometheus.io/docs/practices/)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)
- [Docker build best practices](https://docs.docker.com/build/building/best-practices/)
