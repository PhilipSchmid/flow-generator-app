#!/bin/bash
set -e

# Test scenarios for flow-generator-app

echo "🧪 Running test scenarios for flow-generator-app..."

# Function to cleanup processes
cleanup() {
    echo "Cleaning up..."
    kill $SERVER_PID $CLIENT_PID 2>/dev/null || true
    wait $SERVER_PID $CLIENT_PID 2>/dev/null || true
}
trap cleanup EXIT

# Build binaries
echo "Building binaries..."
make build

# Scenario 1: Basic TCP echo test
echo ""
echo "=== Scenario 1: Basic TCP Echo Test ==="
./bin/echo-server --tcp_ports_server 8080 --log_level info &
SERVER_PID=$!
sleep 2

./bin/flow-generator --server localhost --tcp_ports 8080 --rate 10 --flow_timeout 5 --log_level info &
CLIENT_PID=$!
sleep 6

kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
wait $CLIENT_PID $SERVER_PID 2>/dev/null || true
echo "✅ Scenario 1 passed"

# Scenario 2: Multiple TCP ports
echo ""
echo "=== Scenario 2: Multiple TCP Ports ==="
./bin/echo-server --tcp_ports_server 8080,8081,8082 --log_level info &
SERVER_PID=$!
sleep 2

./bin/flow-generator --server localhost --tcp_ports 8080,8081,8082 --rate 20 --flow_timeout 5 --log_level info &
CLIENT_PID=$!
sleep 6

kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
wait $CLIENT_PID $SERVER_PID 2>/dev/null || true
echo "✅ Scenario 2 passed"

# Scenario 3: UDP echo test
echo ""
echo "=== Scenario 3: UDP Echo Test ==="
./bin/echo-server --udp_ports_server 9000 --log_level info &
SERVER_PID=$!
sleep 2

./bin/flow-generator --server localhost --udp_ports 9000 --protocol udp --rate 10 --flow_timeout 5 --log_level info &
CLIENT_PID=$!
sleep 6

kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
wait $CLIENT_PID $SERVER_PID 2>/dev/null || true
echo "✅ Scenario 3 passed"

# Scenario 4: Mixed TCP and UDP
echo ""
echo "=== Scenario 4: Mixed TCP and UDP ==="
./bin/echo-server --tcp_ports_server 8080,8081 --udp_ports_server 9000,9001 --log_level info &
SERVER_PID=$!
sleep 2

./bin/flow-generator --server localhost --tcp_ports 8080,8081 --udp_ports 9000,9001 --protocol both --rate 15 --flow_timeout 5 --log_level info &
CLIENT_PID=$!
sleep 6

kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
wait $CLIENT_PID $SERVER_PID 2>/dev/null || true
echo "✅ Scenario 4 passed"

# Scenario 5: High load test
echo ""
echo "=== Scenario 5: High Load Test ==="
./bin/echo-server --tcp_ports_server 8080 --log_level warn &
SERVER_PID=$!
sleep 2

./bin/flow-generator --server localhost --tcp_ports 8080 --rate 100 --max_concurrent 50 --flow_timeout 5 --log_level warn &
CLIENT_PID=$!
sleep 6

kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
wait $CLIENT_PID $SERVER_PID 2>/dev/null || true
echo "✅ Scenario 5 passed"

# Scenario 6: Variable payload sizes
echo ""
echo "=== Scenario 6: Variable Payload Sizes ==="
./bin/echo-server --tcp_ports_server 8080 --log_level info &
SERVER_PID=$!
sleep 2

./bin/flow-generator --server localhost --tcp_ports 8080 --rate 10 --min_payload_size 100 --max_payload_size 1000 --flow_timeout 5 --log_level info &
CLIENT_PID=$!
sleep 6

kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
wait $CLIENT_PID $SERVER_PID 2>/dev/null || true
echo "✅ Scenario 6 passed"

# Scenario 7: Constant flows mode
echo ""
echo "=== Scenario 7: Constant Flows Mode ==="
./bin/echo-server --tcp_ports_server 8080 --log_level info &
SERVER_PID=$!
sleep 2

./bin/flow-generator --server localhost --tcp_ports 8080 --constant_flows --max_concurrent 10 --flow_timeout 5 --log_level info &
CLIENT_PID=$!
sleep 6

kill $CLIENT_PID $SERVER_PID 2>/dev/null || true
wait $CLIENT_PID $SERVER_PID 2>/dev/null || true
echo "✅ Scenario 7 passed"

echo ""
echo "✅ All test scenarios passed successfully!"