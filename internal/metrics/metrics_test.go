package metrics

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMetricsEndpoint(t *testing.T) {
	server, err := StartMetricsServer("0")
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, server.Stop(ctx))
	})

	port := server.Addr().(*net.TCPAddr).Port
	resp, err := http.Get("http://127.0.0.1:" + netPort(port) + "/metrics")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMetricsServerReportsBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port
	server, err := StartMetricsServer(netPort(port))
	require.Error(t, err)
	require.Nil(t, server)
}

func netPort(port int) string {
	return strconv.Itoa(port)
}
