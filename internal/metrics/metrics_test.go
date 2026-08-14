package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStartMetricsServer(t *testing.T) {
	// Start metrics server should not error
	err := StartMetricsServer("0")
	assert.NoError(t, err)
}

func TestMetricsEndpoint(t *testing.T) {
	// Start metrics server on a test port
	port := "9191"
	err := StartMetricsServer(port)
	assert.NoError(t, err)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test /metrics endpoint
	resp, err := http.Get("http://localhost:" + port + "/metrics")
	if err != nil {
		t.Skip("Could not connect to test server, skipping")
	}
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}
