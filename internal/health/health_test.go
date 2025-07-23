package health

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthChecker(t *testing.T) {
	checker := NewChecker()

	assert.False(t, checker.ready.Load())
	assert.False(t, checker.healthy.Load())

	checker.SetReady(true)
	assert.True(t, checker.ready.Load())

	checker.SetReady(false)
	assert.False(t, checker.ready.Load())

	checker.SetHealthy(true)
	assert.True(t, checker.healthy.Load())

	checker.SetHealthy(false)
	assert.False(t, checker.healthy.Load())
}

func TestHealthServer(t *testing.T) {
	checker := NewChecker()
	port := "8082"

	err := checker.Start(port)
	require.NoError(t, err)
	defer func() { _ = checker.Stop() }()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:" + port + "/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "OK", string(body))

	resp, err = http.Get("http://localhost:" + port + "/ready")
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "Not Ready", string(body))

	checker.SetReady(true)
	resp, err = http.Get("http://localhost:" + port + "/ready")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "Ready", string(body))

	checker.SetHealthy(false)
	resp, err = http.Get("http://localhost:" + port + "/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "Unhealthy", string(body))
}

func TestHealthServerStop(t *testing.T) {
	checker := NewChecker()
	port := "8083"

	err := checker.Start(port)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:" + port + "/health")
	require.NoError(t, err)
	_ = resp.Body.Close()

	err = checker.Stop()
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	_, err = http.Get("http://localhost:" + port + "/health")
	assert.Error(t, err)

	assert.False(t, checker.ready.Load())
	assert.False(t, checker.healthy.Load())
}

func TestStopWithoutStart(t *testing.T) {
	checker := NewChecker()

	err := checker.Stop()
	assert.NoError(t, err)
}

func TestConcurrentStateChanges(t *testing.T) {
	checker := NewChecker()

	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 1000; i++ {
			checker.SetReady(i%2 == 0)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			checker.SetHealthy(i%2 == 0)
		}
		done <- true
	}()

	for i := 0; i < 2; i++ {
		<-done
	}

	// If we get here without race conditions, concurrent access works
	assert.True(t, true)
}
