# Development Guide

This guide provides information for developers working on the flow-generator-app project.

## Table of Contents
- [Project Structure](#project-structure)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Testing](#testing)
- [Code Quality](#code-quality)
- [Contributing](#contributing)

## Project Structure

```
flow-generator-app/
├── cmd/                    # Application entry points
│   ├── client/            # Flow generator client
│   └── server/            # Echo server
├── internal/              # Private application code
│   ├── config/           # Configuration management
│   ├── handlers/         # Protocol handlers (TCP/UDP)
│   ├── health/           # Health check server
│   ├── logging/          # Logging utilities
│   ├── metrics/          # Prometheus metrics
│   ├── server/           # Server implementations
│   ├── tracing/          # OpenTelemetry tracing
│   └── version/          # Version information
├── k8s/                   # Kubernetes manifests
├── scripts/               # Utility scripts
└── .github/workflows/     # CI/CD pipelines
```

## Getting Started

### Prerequisites
- Go 1.25 or later
- Docker (for container builds)
- Make

### Initial Setup

1. Clone the repository:
```bash
git clone https://github.com/PhilipSchmid/flow-generator-app.git
cd flow-generator-app
```

2. Install development tools:
```bash
make install-tools
```

3. Download dependencies:
```bash
make deps
```

4. Run tests to verify setup:
```bash
make test
```

## Development Workflow

### Live Development
The project supports live reload for rapid development:

```bash
# Run server with live reload (recommended for development)
make dev

# Run server with live reload (same as 'make dev')
make dev-server

# Run client with live reload (in a separate terminal)
make dev-client
```

For typical development, run `make dev` in one terminal to start the server, then manually run the client in another terminal when you need to test.

### Building
```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Build specific component
make build-server
make build-client
```

### Running Locally
```bash
# Using make
make quick-test

# Manual execution
./bin/echo-server --tcp_ports_server 8080,8081 --udp_ports_server 9000
./bin/flow-generator --server localhost --tcp_ports 8080,8081 --rate 10
```

### Configuration
The application supports configuration through:
1. Command-line flags
2. Environment variables (prefix: `FLOW_GENERATOR_`)
3. Configuration files

Example:
```bash
# Using environment variables
export FLOW_GENERATOR_LOG_LEVEL=debug
export FLOW_GENERATOR_METRICS_PORT=9090

# Using flags
./bin/echo-server --log_level debug --metrics_port 9090
```

## Testing

### Unit Tests
```bash
# Run all tests
make test

# Run with verbose output
make test-verbose

# Run with race detector
make test-race

# Generate coverage report
make test-coverage
```

### Benchmarks
```bash
make benchmark
```

### Integration Tests
```bash
make quick-test
```

## Code Quality

### Linting
The project uses golangci-lint for code quality checks:

```bash
make lint
```

### Formatting
```bash
# Format all code
make fmt

# Run go vet
make vet
```

### Pre-commit Checklist
Before committing:
1. Run `make fmt` to format code
2. Run `make lint` to check for issues
3. Run `make test` to ensure tests pass
4. Update documentation if needed

## Contributing

### Git Workflow
1. Create a feature branch from `main`
2. Make your changes
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

### Commit Messages
Follow conventional commit format:
- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `test:` Test additions/changes
- `refactor:` Code refactoring
- `chore:` Maintenance tasks

### Pull Request Process
1. Update the README.md with details of changes if applicable
2. Ensure CI/CD checks pass
3. Request review from maintainers
4. Squash commits before merging

## Docker Development

### Building Images
```bash
# Build all images
make docker-build

# Run containers locally
make docker-run
```

### Multi-platform Builds
The Dockerfiles support multi-platform builds (linux/amd64, linux/arm64).

## Debugging

### Enable Debug Logging
```bash
./bin/echo-server --log_level debug
```

### Endpoints
- Prometheus metrics: `http://localhost:9090/metrics`
- Health check: `http://localhost:8082/health`
- Readiness check: `http://localhost:8082/ready`

### Common Issues

1. **Port already in use**: Check for existing processes using the port
2. **Permission denied**: Some ports (< 1024) require elevated privileges
3. **Connection refused**: Ensure the server is running and accessible

## Performance Testing

### Load Testing
```bash
# High rate test
./bin/flow-generator --server localhost --rate 1000 --max_concurrent 500

# Long duration test
./bin/flow-generator --server localhost --min_duration 60 --max_duration 300
```

### Monitoring
- Use Prometheus metrics for monitoring performance
- Enable tracing for distributed tracing analysis

## Release Process

1. Update version numbers
2. Update CHANGELOG.md
3. Create and push a tag: `git tag v1.2.3 && git push origin v1.2.3`
4. GitHub Actions will automatically:
   - Run tests
   - Build and push Docker images
   - Create a GitHub release

## Additional Resources

- [Go Documentation](https://golang.org/doc/)
- [Prometheus Best Practices](https://prometheus.io/docs/practices/)
- [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/)
- [Docker Best Practices](https://docs.docker.com/develop/dev-best-practices/)