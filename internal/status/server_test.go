package status

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusServerIsLoopbackAndGetOnly(t *testing.T) {
	now := time.Now().UTC()
	server, err := startAt("127.0.0.1:0", func() Snapshot {
		return Snapshot{SchemaVersion: SchemaVersion, Role: "client", Version: "test", SampledAt: now, StartedAt: now}
	})
	require.NoError(t, err)
	address, ok := server.Addr().(*net.TCPAddr)
	require.True(t, ok)
	assert.True(t, address.IP.IsLoopback())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, server.Stop(ctx))
	})

	endpoint := "http://" + server.Addr().String() + Path
	response, err := http.Get(endpoint) // #nosec G107 -- test uses a loopback listener created above
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	var snapshot Snapshot
	require.NoError(t, json.NewDecoder(response.Body).Decode(&snapshot))
	assert.Equal(t, "client", snapshot.Role)

	request, err := http.NewRequest(http.MethodPost, endpoint, nil)
	require.NoError(t, err)
	postResponse, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = postResponse.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, postResponse.StatusCode)
	assert.Equal(t, http.MethodGet, postResponse.Header.Get("Allow"))
}

func TestStatusServerCanBeDisabled(t *testing.T) {
	server, err := Start("0", func() Snapshot { return Snapshot{} })
	require.NoError(t, err)
	assert.Nil(t, server)
}

func TestStatusServerRequiresProvider(t *testing.T) {
	server, err := startAt("127.0.0.1:0", nil)
	assert.Nil(t, server)
	assert.Error(t, err)
}
