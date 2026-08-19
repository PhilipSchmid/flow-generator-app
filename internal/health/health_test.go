package health

import (
	"io"
	"net"
	"net/http"
	"strconv"
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

	err := checker.Start("0")
	require.NoError(t, err)
	defer func() { _ = checker.Stop() }()
	port := strconv.Itoa(checker.Addr().(*net.TCPAddr).Port)

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

	err := checker.Start("0")
	require.NoError(t, err)
	port := strconv.Itoa(checker.Addr().(*net.TCPAddr).Port)

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

func TestHealthServerReportsBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	checker := NewChecker()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	require.Error(t, checker.Start(port))
	assert.False(t, checker.healthy.Load())
}

func TestHealthServerClearsStateWhenListenerFails(t *testing.T) {
	checker := NewChecker()
	require.NoError(t, checker.Start("0"))
	checker.SetReady(true)
	require.NoError(t, checker.listener.Close())
	require.Eventually(t, func() bool {
		return !checker.Healthy() && !checker.Ready()
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, checker.Stop())
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
