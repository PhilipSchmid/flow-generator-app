#!/bin/bash
set -e

echo "🚀 Setting up development environment for flow-generator-app..."

# Check Go version
echo "Checking Go version..."
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED_VERSION="1.24"

if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]; then
    echo "❌ Go version $REQUIRED_VERSION or higher is required (found $GO_VERSION)"
    exit 1
fi
echo "✅ Go version $GO_VERSION"

# Install development tools
echo "Installing development tools..."
make install-tools

# Download dependencies
echo "Downloading Go dependencies..."
make deps

# Run initial build
echo "Running initial build..."
make build

# Run tests
echo "Running tests..."
make test

# Setup git hooks (optional)
if [ -d .git ]; then
    echo "Setting up git hooks..."
    cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash
# Pre-commit hook for flow-generator-app

echo "Running pre-commit checks..."

# Format check
if ! make fmt; then
    echo "❌ Code formatting issues found. Please run 'make fmt'"
    exit 1
fi

# Lint check
if ! make lint; then
    echo "❌ Linting issues found. Please fix them before committing"
    exit 1
fi

# Test check
if ! make test; then
    echo "❌ Tests failed. Please fix them before committing"
    exit 1
fi

echo "✅ All pre-commit checks passed!"
EOF
    chmod +x .git/hooks/pre-commit
    echo "✅ Git hooks installed"
fi

echo ""
echo "✅ Development environment setup complete!"
echo ""
echo "Quick start commands:"
echo "  make dev          - Run server and client with live reload"
echo "  make test         - Run tests"
echo "  make lint         - Run linters"
echo "  make help         - Show all available commands"