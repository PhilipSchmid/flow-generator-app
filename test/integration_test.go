package test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findAvailablePort finds an available port for testing
func findAvailablePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func findAvailableUDPPort(t *testing.T) int {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func waitForReady(t *testing.T, port int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/ready", port)
	require.Eventually(t, func() bool {
		client := http.Client{Timeout: 200 * time.Millisecond}
		resp, err := client.Get(url)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 25*time.Millisecond)
}

// TestServerClientIntegration tests the server and client together
func TestServerClientIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Find available ports
	tcpPort := findAvailablePort(t)
	udpPort := findAvailableUDPPort(t)
	metricsPort := findAvailablePort(t)
	healthPort := findAvailablePort(t)

	// Build server and client binaries
	serverBinary := "./test-server"
	clientBinary := "./test-client"

	// Build server
	cmd := exec.Command("go", "build", "-o", serverBinary, "../cmd/server")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build server: %s", string(output))
	defer func() { _ = os.Remove(serverBinary) }()

	// Build client
	cmd = exec.Command("go", "build", "-o", clientBinary, "../cmd/client")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build client: %s", string(output))
	defer func() { _ = os.Remove(clientBinary) }()

	// Start server
	serverCmd := exec.Command(serverBinary,
		"--tcp-ports-server", fmt.Sprintf("%d", tcpPort),
		"--udp-ports-server", fmt.Sprintf("%d", udpPort),
		"--metrics-port", fmt.Sprintf("%d", metricsPort),
		"--health-port", fmt.Sprintf("%d", healthPort),
		"--log-level", "error",
		"--log-format", "json",
	)

	var serverOutput bytes.Buffer
	serverCmd.Stdout = &serverOutput
	serverCmd.Stderr = &serverOutput

	err = serverCmd.Start()
	require.NoError(t, err)
	defer func() { _ = serverCmd.Process.Kill() }()

	waitForReady(t, healthPort)

	// Test TCP client
	t.Run("TCP Client", func(t *testing.T) {
		clientCmd := exec.Command(clientBinary,
			"--server", "127.0.0.1",
			"--tcp-ports", fmt.Sprintf("%d", tcpPort),
			"--rate", "10",
			"--max-concurrent", "2",
			"--flow-count", "5",
			"--min-duration", "0.1",
			"--max-duration", "0.2",
			"--payload-size", "100",
			"--log-level", "error",
		)

		output, err := clientCmd.CombinedOutput()
		assert.NoError(t, err, "Client failed: %s", string(output))
	})

	// Test UDP client
	t.Run("UDP Client with legacy flag names", func(t *testing.T) {
		clientCmd := exec.Command(clientBinary,
			"--server", "127.0.0.1",
			"--udp_ports", fmt.Sprintf("%d", udpPort),
			"--protocol", "udp",
			"--rate", "10",
			"--max_concurrent", "2",
			"--flow_count", "5",
			"--min_duration", "0.1",
			"--max_duration", "0.2",
			"--payload_size", "100",
			"--log_level", "error",
		)

		output, err := clientCmd.CombinedOutput()
		assert.NoError(t, err, "Client failed: %s", string(output))
	})

	// Test metrics endpoint
	t.Run("Metrics Endpoint", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", metricsPort))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, string(body), "active_tcp_connections")
	})

	// Clean up
	_ = serverCmd.Process.Kill()
	_ = serverCmd.Wait()
}

// TestServerTCPEcho tests TCP echo functionality
func TestServerTCPEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tcpPort := findAvailablePort(t)
	healthPort := findAvailablePort(t)
	serverBinary := "./test-server-tcp"

	// Build server
	cmd := exec.Command("go", "build", "-o", serverBinary, "../cmd/server")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build server: %s", string(output))
	defer func() { _ = os.Remove(serverBinary) }()

	// Keep the old underscore spellings covered for existing deployments.
	serverCmd := exec.Command(serverBinary,
		"--tcp_ports_server", fmt.Sprintf("%d", tcpPort),
		"--health_port", fmt.Sprintf("%d", healthPort),
		"--log_level", "error",
	)

	err = serverCmd.Start()
	require.NoError(t, err)
	defer func() { _ = serverCmd.Process.Kill() }()

	waitForReady(t, healthPort)

	// Connect and test echo
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	testData := []byte("Hello, TCP Server!")
	_, err = conn.Write(testData)
	require.NoError(t, err)

	buf := make([]byte, len(testData))
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)

	assert.Equal(t, testData, buf)

	_ = serverCmd.Process.Kill()
}

// TestServerUDPEcho tests UDP echo functionality
func TestServerUDPEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	udpPort := findAvailableUDPPort(t)
	healthPort := findAvailablePort(t)
	serverBinary := "./test-server-udp"

	// Build server
	cmd := exec.Command("go", "build", "-o", serverBinary, "../cmd/server")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build server: %s", string(output))
	defer func() { _ = os.Remove(serverBinary) }()

	// Start server
	serverCmd := exec.Command(serverBinary,
		"--udp-ports-server", fmt.Sprintf("%d", udpPort),
		"--health-port", fmt.Sprintf("%d", healthPort),
		"--log-level", "error",
	)

	err = serverCmd.Start()
	require.NoError(t, err)
	defer func() { _ = serverCmd.Process.Kill() }()

	waitForReady(t, healthPort)

	// Connect and test echo
	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", udpPort))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	testData := []byte("Hello, UDP Server!")
	_, err = conn.Write(testData)
	require.NoError(t, err)

	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	require.NoError(t, err)

	assert.Equal(t, testData, buf[:n])

	_ = serverCmd.Process.Kill()
}

// TestMultipleFlows tests multiple concurrent flows
func TestMultipleFlows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tcpPort1 := findAvailablePort(t)
	tcpPort2 := findAvailablePort(t)
	udpPort1 := findAvailableUDPPort(t)
	healthPort := findAvailablePort(t)
	serverBinary := "./test-server-multi"
	clientBinary := "./test-client-multi"

	// Build binaries
	cmd := exec.Command("go", "build", "-o", serverBinary, "../cmd/server")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build server: %s", string(output))
	defer func() { _ = os.Remove(serverBinary) }()

	cmd = exec.Command("go", "build", "-o", clientBinary, "../cmd/client")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to build client: %s", string(output))
	defer func() { _ = os.Remove(clientBinary) }()

	// Start server with multiple ports
	serverCmd := exec.Command(serverBinary,
		"--tcp-ports-server", fmt.Sprintf("%d,%d", tcpPort1, tcpPort2),
		"--udp-ports-server", fmt.Sprintf("%d", udpPort1),
		"--health-port", fmt.Sprintf("%d", healthPort),
		"--log-level", "error",
	)

	err = serverCmd.Start()
	require.NoError(t, err)
	defer func() { _ = serverCmd.Process.Kill() }()

	waitForReady(t, healthPort)

	// Run client with multiple flows
	clientCmd := exec.Command(clientBinary,
		"--server", "127.0.0.1",
		"--tcp-ports", fmt.Sprintf("%d,%d", tcpPort1, tcpPort2),
		"--udp-ports", fmt.Sprintf("%d", udpPort1),
		"--rate", "20",
		"--max-concurrent", "5",
		"--flow-count", "10",
		"--min-duration", "0.1",
		"--max-duration", "0.3",
		"--payload-size", "500",
		"--log-level", "info",
	)

	output, err = clientCmd.CombinedOutput()
	assert.NoError(t, err, "Client failed: %s", string(output))

	// Verify flows were generated
	outputStr := string(output)

	// Check that flows were completed successfully
	assert.Contains(t, outputStr, "Flow count limit reached; final flows drained")
	assert.Contains(t, outputStr, "Total Requests Sent")

	// Verify requests were sent - check that we have non-zero values
	assert.Contains(t, outputStr, "│ Total Requests Sent")
	assert.NotContains(t, outputStr, "│ Total Requests Sent         │ 0")
	assert.Contains(t, outputStr, "│ Total TCP Requests Sent")
	assert.Contains(t, outputStr, "│ Total UDP Requests Sent")

	_ = serverCmd.Process.Kill()
}

// BenchmarkTCPFlow benchmarks TCP flow performance
func BenchmarkTCPFlow(b *testing.B) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(b, err)
	tcpPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	serverBinary := "./bench-server"
	clientBinary := "./bench-client"

	// Build binaries
	cmd := exec.Command("go", "build", "-o", serverBinary, "../cmd/server")
	output, err := cmd.CombinedOutput()
	require.NoError(b, err, "Failed to build server: %s", string(output))
	defer func() { _ = os.Remove(serverBinary) }()

	cmd = exec.Command("go", "build", "-o", clientBinary, "../cmd/client")
	output, err = cmd.CombinedOutput()
	require.NoError(b, err, "Failed to build client: %s", string(output))
	defer func() { _ = os.Remove(clientBinary) }()

	// Start server
	serverCmd := exec.Command(serverBinary,
		"--tcp-ports-server", fmt.Sprintf("%d", tcpPort),
		"--log-level", "error",
	)
	err = serverCmd.Start()
	require.NoError(b, err)
	defer func() { _ = serverCmd.Process.Kill() }()

	time.Sleep(1 * time.Second)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		clientCmd := exec.Command(clientBinary,
			"--server", "127.0.0.1",
			"--tcp-ports", fmt.Sprintf("%d", tcpPort),
			"--rate", "100",
			"--max-concurrent", "10",
			"--flow-count", "1",
			"--min-duration", "0.01",
			"--max-duration", "0.01",
			"--payload-size", "1024",
			"--log-level", "error",
		)

		err := clientCmd.Run()
		if err != nil {
			b.Fatal(err)
		}
	}

	_ = serverCmd.Process.Kill()
}
