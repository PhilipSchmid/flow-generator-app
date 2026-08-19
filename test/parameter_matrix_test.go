package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientServerParameterMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration parameter matrix in short mode")
	}

	serverBinary := buildSentinelBinary(t, "echo-server", "../cmd/server")
	clientBinary := buildSentinelBinary(t, "flow-generator", "../cmd/client")

	t.Run("TCP only with fixed payload", func(t *testing.T) {
		tcpPort := findAvailablePort(t)
		server := startSentinelServer(t, serverBinary,
			"--tcp-ports-server", strconv.Itoa(tcpPort),
			"--udp-ports-server=",
		)

		output := runSentinelClient(t, clientBinary,
			"--server", "127.0.0.1",
			"--protocol", "tcp",
			"--tcp-ports", strconv.Itoa(tcpPort),
			"--udp-ports=",
			"--rate", "250",
			"--max-concurrent", "8",
			"--flow-count", "8",
			"--min-duration", "0.02",
			"--max-duration", "0.02",
			"--payload-size", "64",
		)

		assert.Equal(t, uint64(8), clientSummaryRequestsTX(t, output, "TOTAL"))
		assert.Equal(t, uint64(8), clientSummaryRequestsTX(t, output, "TCP"))
		assert.Equal(t, uint64(0), clientSummaryRequestsTX(t, output, "UDP"))

		metrics := scrapeSentinelMetrics(t, server.metricsPort)
		assert.Equal(t, float64(8), prometheusValue(t, metrics, "requests_received_total", "tcp", tcpPort))
		assert.Equal(t, float64(8*64), prometheusValue(t, metrics, "bytes_received_total", "tcp", tcpPort))
	})

	t.Run("UDP only with one packet per flow", func(t *testing.T) {
		udpPort := findAvailableUDPPort(t)
		server := startSentinelServer(t, serverBinary,
			"--tcp-ports-server=",
			"--udp-ports-server", strconv.Itoa(udpPort),
		)

		output := runSentinelClient(t, clientBinary,
			"--server", "127.0.0.1",
			"--protocol", "udp",
			"--tcp-ports=",
			"--udp-ports", strconv.Itoa(udpPort),
			"--rate", "200",
			"--max-concurrent", "6",
			"--flow-count", "6",
			"--min-duration", "0.01",
			"--max-duration", "0.01",
			"--payload-size", "128",
		)

		assert.Equal(t, uint64(6), clientSummaryRequestsTX(t, output, "TOTAL"))
		assert.Equal(t, uint64(0), clientSummaryRequestsTX(t, output, "TCP"))
		assert.Equal(t, uint64(6), clientSummaryRequestsTX(t, output, "UDP"))

		metrics := scrapeSentinelMetrics(t, server.metricsPort)
		assert.Equal(t, float64(6), prometheusValue(t, metrics, "requests_received_total", "udp", udpPort))
		assert.Equal(t, float64(6*128), prometheusValue(t, metrics, "bytes_received_total", "udp", udpPort))
	})

	t.Run("mixed protocols", func(t *testing.T) {
		tcpPort := findAvailablePort(t)
		udpPort := findAvailableUDPPort(t)
		server := startSentinelServer(t, serverBinary,
			"--tcp-ports-server", strconv.Itoa(tcpPort),
			"--udp-ports-server", strconv.Itoa(udpPort),
		)

		output := runSentinelClient(t, clientBinary,
			"--server", "127.0.0.1",
			"--protocol", "both",
			"--tcp-ports", strconv.Itoa(tcpPort),
			"--udp-ports", strconv.Itoa(udpPort),
			"--rate", "1000",
			"--max-concurrent", "100",
			"--flow-count", "100",
			"--min-duration", "0.01",
			"--max-duration", "0.01",
			"--payload-size", "32",
		)

		tcpSent := clientSummaryRequestsTX(t, output, "TCP")
		udpSent := clientSummaryRequestsTX(t, output, "UDP")
		assert.Positive(t, tcpSent)
		assert.Positive(t, udpSent)
		assert.Equal(t, uint64(100), tcpSent+udpSent)

		metrics := scrapeSentinelMetrics(t, server.metricsPort)
		tcpReceived := prometheusValue(t, metrics, "requests_received_total", "tcp", tcpPort)
		udpReceived := prometheusValue(t, metrics, "requests_received_total", "udp", udpPort)
		assert.Equal(t, float64(tcpSent), tcpReceived)
		assert.Equal(t, float64(udpSent), udpReceived)
		assert.Equal(t, float64(100*32),
			prometheusValue(t, metrics, "bytes_received_total", "tcp", tcpPort)+
				prometheusValue(t, metrics, "bytes_received_total", "udp", udpPort),
		)
	})

	t.Run("multiple TCP ports", func(t *testing.T) {
		firstPort := findAvailablePort(t)
		secondPort := findUniqueSentinelPort(t, map[int]struct{}{firstPort: {}})
		server := startSentinelServer(t, serverBinary,
			"--tcp-ports-server", fmt.Sprintf("%d,%d", firstPort, secondPort),
			"--udp-ports-server=",
		)

		output := runSentinelClient(t, clientBinary,
			"--server", "127.0.0.1",
			"--protocol", "tcp",
			"--tcp-ports", fmt.Sprintf("%d,%d", firstPort, secondPort),
			"--rate", "1000",
			"--max-concurrent", "100",
			"--flow-count", "100",
			"--min-duration", "0.01",
			"--max-duration", "0.01",
			"--payload-size", "48",
		)

		assert.Equal(t, uint64(100), clientSummaryRequestsTX(t, output, "TCP"))
		metrics := scrapeSentinelMetrics(t, server.metricsPort)
		firstReceived := prometheusValue(t, metrics, "requests_received_total", "tcp", firstPort)
		secondReceived := prometheusValue(t, metrics, "requests_received_total", "tcp", secondPort)
		assert.Positive(t, firstReceived)
		assert.Positive(t, secondReceived)
		assert.Equal(t, float64(100), firstReceived+secondReceived)
	})

	t.Run("capacity pressure drops starts but reaches flow count", func(t *testing.T) {
		tcpPort := findAvailablePort(t)
		server := startSentinelServer(t, serverBinary,
			"--tcp-ports-server", strconv.Itoa(tcpPort),
			"--udp-ports-server=",
		)

		output := runSentinelClient(t, clientBinary,
			"--server", "127.0.0.1",
			"--protocol", "tcp",
			"--tcp-ports", strconv.Itoa(tcpPort),
			"--rate", "1000",
			"--max-concurrent", "2",
			"--flow-count", "10",
			"--min-duration", "0.05",
			"--max-duration", "0.05",
			"--payload-size", "16",
			"--log-level", "info",
		)

		assert.Equal(t, uint64(10), clientSummaryRequestsTX(t, output, "TCP"))
		assert.Positive(t, structuredLogValue(t, output, "starts_skipped_at_capacity"))
		metrics := scrapeSentinelMetrics(t, server.metricsPort)
		assert.Equal(t, float64(10), prometheusValue(t, metrics, "requests_received_total", "tcp", tcpPort))
	})

	t.Run("variable TCP payload range", func(t *testing.T) {
		tcpPort := findAvailablePort(t)
		server := startSentinelServer(t, serverBinary,
			"--tcp-ports-server", strconv.Itoa(tcpPort),
			"--udp-ports-server=",
		)

		const flowCount = 20
		output := runSentinelClient(t, clientBinary,
			"--server", "127.0.0.1",
			"--protocol", "tcp",
			"--tcp-ports", strconv.Itoa(tcpPort),
			"--rate", "500",
			"--max-concurrent", "20",
			"--flow-count", strconv.Itoa(flowCount),
			"--min-duration", "0.01",
			"--max-duration", "0.01",
			"--min-payload-size", "64",
			"--max-payload-size", "128",
		)

		assert.Equal(t, uint64(flowCount), clientSummaryRequestsTX(t, output, "TCP"))
		metrics := scrapeSentinelMetrics(t, server.metricsPort)
		bytesReceived := prometheusValue(t, metrics, "bytes_received_total", "tcp", tcpPort)
		assert.GreaterOrEqual(t, bytesReceived, float64(flowCount*64))
		assert.LessOrEqual(t, bytesReceived, float64(flowCount*128))
	})

	t.Run("live dashboard status for mixed traffic", func(t *testing.T) {
		tcpPort := findAvailablePort(t)
		udpPort := findAvailableUDPPort(t)
		server := startSentinelServer(t, serverBinary,
			"--tcp-ports-server", strconv.Itoa(tcpPort),
			"--udp-ports-server", strconv.Itoa(udpPort),
		)

		usedPorts := map[int]struct{}{tcpPort: {}, udpPort: {}, server.metricsPort: {}, server.statusPort: {}}
		statusPort := findUniqueSentinelPort(t, usedPorts)
		usedPorts[statusPort] = struct{}{}
		metricsPort := findUniqueSentinelPort(t, usedPorts)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, clientBinary,
			"--server", "127.0.0.1",
			"--protocol", "both",
			"--tcp-ports", strconv.Itoa(tcpPort),
			"--udp-ports", strconv.Itoa(udpPort),
			"--rate", "100",
			"--max-concurrent", "200",
			"--flow-count", "100",
			"--min-duration", "1",
			"--max-duration", "1",
			"--payload-size", "64",
			"--metrics-port", strconv.Itoa(metricsPort),
			"--status-port", strconv.Itoa(statusPort),
			"--log-level", "error",
		)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		require.NoError(t, cmd.Start())
		clientSnapshot := waitForSentinelStatus(t, statusPort, func(snapshot statusapi.Snapshot) bool {
			return snapshot.Client != nil && snapshot.Client.FlowsStarted > 0 &&
				snapshot.Client.TCPLatency.Count > 0 && snapshot.Client.UDPLatency.Count > 0
		})
		assert.Equal(t, "client", clientSnapshot.Role)
		assert.Equal(t, float64(100), clientSnapshot.Configuration.Rate)
		assert.Positive(t, clientSnapshot.Client.FlowsActive)
		assert.Len(t, clientSnapshot.Client.PortFlows, 2)

		err := cmd.Wait()
		require.NoError(t, err, "client failed: %s", output.String())
		require.NoError(t, ctx.Err())

		serverSnapshot := waitForSentinelStatus(t, server.statusPort, func(snapshot statusapi.Snapshot) bool {
			return snapshot.Server != nil && snapshot.Traffic.TotalTCPReceived > 0 && snapshot.Traffic.TotalUDPReceived > 0
		})
		assert.Equal(t, "server", serverSnapshot.Role)
		assert.True(t, serverSnapshot.Server.Ready)
		assert.True(t, serverSnapshot.Server.Healthy)
	})

	t.Run("invalid UDP payload is rejected before dialing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, clientBinary,
			"--protocol", "udp",
			"--tcp-ports=",
			"--udp-ports", strconv.Itoa(findAvailableUDPPort(t)),
			"--payload-size", "1501",
			"--mtu", "1500",
		)
		output, err := cmd.CombinedOutput()
		require.Error(t, err)
		assert.Contains(t, string(output), "UDP payload size cannot exceed MTU")
		assert.NoError(t, ctx.Err())
	})
}

type sentinelServer struct {
	metricsPort int
	statusPort  int
}

func buildSentinelBinary(t *testing.T, name, packagePath string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	cmd := exec.Command("go", "build", "-o", path, packagePath)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "build %s: %s", name, string(output))
	return path
}

func startSentinelServer(t *testing.T, binary string, protocolArgs ...string) sentinelServer {
	t.Helper()
	usedPorts := sentinelPorts(protocolArgs)
	metricsPort := findUniqueSentinelPort(t, usedPorts)
	usedPorts[metricsPort] = struct{}{}
	healthPort := findUniqueSentinelPort(t, usedPorts)
	usedPorts[healthPort] = struct{}{}
	statusPort := findUniqueSentinelPort(t, usedPorts)
	args := append([]string{}, protocolArgs...)
	args = append(args,
		"--metrics-port", strconv.Itoa(metricsPort),
		"--health-port", strconv.Itoa(healthPort),
		"--status-port", strconv.Itoa(statusPort),
		"--log-level", "error",
	)

	cmd := exec.Command(binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	waitForReady(t, healthPort)
	return sentinelServer{metricsPort: metricsPort, statusPort: statusPort}
}

func runSentinelClient(t *testing.T, binary string, args ...string) string {
	t.Helper()
	metricsPort := findAvailablePort(t)
	args = append(args,
		"--metrics-port", strconv.Itoa(metricsPort),
		"--status-port", "0",
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "client failed: %s", string(output))
	require.NoError(t, ctx.Err())
	return string(output)
}

func waitForSentinelStatus(t *testing.T, port int, ready func(statusapi.Snapshot) bool) statusapi.Snapshot {
	t.Helper()
	client := http.Client{Timeout: 250 * time.Millisecond}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d%s", port, statusapi.Path)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			var snapshot statusapi.Snapshot
			decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && ready(snapshot) {
				return snapshot
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("status endpoint %s did not become ready", endpoint)
	return statusapi.Snapshot{}
}

func sentinelPorts(args []string) map[int]struct{} {
	ports := make(map[int]struct{})
	for _, arg := range args {
		for _, value := range strings.Split(arg, ",") {
			value = strings.TrimPrefix(value, "--tcp-ports-server=")
			value = strings.TrimPrefix(value, "--udp-ports-server=")
			if port, err := strconv.Atoi(value); err == nil {
				ports[port] = struct{}{}
			}
		}
	}
	return ports
}

func findUniqueSentinelPort(t testing.TB, used map[int]struct{}) int {
	t.Helper()
	for range 20 {
		port := findAvailablePort(t)
		if _, exists := used[port]; !exists {
			return port
		}
	}
	t.Fatal("could not find a distinct TCP port")
	return 0
}

func scrapeSentinelMetrics(t *testing.T, port int) string {
	t.Helper()
	client := http.Client{Timeout: time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return string(body)
}

func prometheusValue(t *testing.T, metrics, name, protocol string, port int) float64 {
	t.Helper()
	protocolLabel := `protocol="` + protocol + `"`
	portLabel := `port="` + strconv.Itoa(port) + `"`
	for _, line := range strings.Split(metrics, "\n") {
		if !strings.HasPrefix(line, name+"{") ||
			!strings.Contains(line, protocolLabel) ||
			!strings.Contains(line, portLabel) {
			continue
		}
		fields := strings.Fields(line)
		require.NotEmpty(t, fields)
		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		require.NoError(t, err)
		return value
	}
	t.Fatalf("metric %s{%s,%s} not found", name, protocolLabel, portLabel)
	return 0
}

func clientSummaryRequestsTX(t *testing.T, output, protocol string) uint64 {
	t.Helper()
	inSummary := false
	for _, line := range strings.Split(output, "\n") {
		switch strings.TrimSpace(line) {
		case "RUN SUMMARY":
			inSummary = true
			continue
		case "PORT BREAKDOWN":
			inSummary = false
		}
		if !inSummary {
			continue
		}
		cells := strings.Split(line, "│")
		if len(cells) < 7 || strings.TrimSpace(cells[1]) != protocol {
			continue
		}
		value := strings.ReplaceAll(strings.TrimSpace(cells[3]), ",", "")
		if value == "—" {
			return 0
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		require.NoError(t, err)
		return parsed
	}
	if protocol == "TOTAL" {
		t.Fatalf("summary protocol %q not found in:\n%s", protocol, output)
	}
	return 0
}

func structuredLogValue(t *testing.T, output, name string) uint64 {
	t.Helper()
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(name) + `":\s*([0-9]+)`)
	matches := pattern.FindAllStringSubmatch(output, -1)
	require.NotEmpty(t, matches, "log field %q not found in:\n%s", name, output)
	value, err := strconv.ParseUint(matches[len(matches)-1][1], 10, 64)
	require.NoError(t, err)
	return value
}
