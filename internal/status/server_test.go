package status

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
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
	assert.Equal(t, "same-origin", response.Header.Get("Cross-Origin-Resource-Policy"))
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

func TestStatusServerRejectsUntrustedHostAndOrigin(t *testing.T) {
	server, err := startAt("127.0.0.1:0", func() Snapshot { return Snapshot{SchemaVersion: SchemaVersion} })
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, server.Stop(ctx))
	})
	endpoint := "http://" + server.Addr().String() + Path

	for name, testCase := range map[string]struct {
		host   string
		origin string
	}{
		"rebinding host": {host: "attacker.example:" + strconv.Itoa(server.Addr().(*net.TCPAddr).Port)},
		"cross origin":   {host: server.Addr().String(), origin: "https://attacker.example"},
	} {
		t.Run(name, func(t *testing.T) {
			request, requestErr := http.NewRequest(http.MethodGet, endpoint, nil)
			require.NoError(t, requestErr)
			request.Host = testCase.host
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			response, requestErr := http.DefaultClient.Do(request)
			require.NoError(t, requestErr)
			defer func() { _ = response.Body.Close() }()
			assert.Equal(t, http.StatusForbidden, response.StatusCode)
		})
	}
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
