package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name, input, expected string
		wantErr               bool
	}{
		{name: "loopback", input: "http://127.0.0.1:9191", expected: "http://127.0.0.1:9191/api/v1/status"},
		{name: "localhost", input: "http://localhost:9190/api/v1/status", expected: "http://127.0.0.1:9190/api/v1/status"},
		{name: "IPv6 loopback", input: "http://[::1]:9190", expected: "http://[::1]:9190/api/v1/status"},
		{name: "remote", input: "http://example.com:9190", wantErr: true},
		{name: "credentials", input: "http://user:pass@127.0.0.1:9190", wantErr: true},
		{name: "https", input: "https://127.0.0.1:9190", wantErr: true},
		{name: "wrong path", input: "http://127.0.0.1:9190/metrics", wantErr: true},
		{name: "missing port", input: "http://127.0.0.1", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := normalizeEndpoint(test.input)
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestClientFetchValidatesStatusContract(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name        string
		contentType string
		payload     any
		wantErr     string
	}{
		{
			name: "valid client",
			payload: statusapi.Snapshot{
				SchemaVersion: statusapi.SchemaVersion, Role: "client", SampledAt: now, StartedAt: now,
				Client: &statusapi.ClientSnapshot{},
			},
		},
		{
			name: "missing role data",
			payload: statusapi.Snapshot{
				SchemaVersion: statusapi.SchemaVersion, Role: "server", SampledAt: now, StartedAt: now,
			},
			wantErr: "missing server data",
		},
		{
			name:        "wrong content type",
			contentType: "text/plain",
			payload:     statusapi.Snapshot{},
			wantErr:     "unexpected content type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				contentType := test.contentType
				if contentType == "" {
					contentType = "application/json"
				}
				w.Header().Set("Content-Type", contentType)
				_ = json.NewEncoder(w).Encode(test.payload)
			}))
			defer server.Close()

			client, err := NewClient(server.URL)
			require.NoError(t, err)
			_, err = client.Fetch()
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat(" ", maxStatusResponseBytes+1)))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	_, err = client.Fetch()
	require.ErrorContains(t, err, "exceeds 1 MiB")
}

func TestClientStripsTerminalControlsFromStatusText(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statusapi.Snapshot{
			SchemaVersion: statusapi.SchemaVersion,
			Role:          "client",
			Version:       "v1\x1b]8;;https://example.invalid\a",
			State:         "run\nning",
			SampledAt:     now,
			StartedAt:     now,
			Configuration: statusapi.Configuration{Target: "echo\x1b[31m.invalid"},
			Client:        &statusapi.ClientSnapshot{},
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	snapshot, err := client.Fetch()
	require.NoError(t, err)
	assert.NotContains(t, snapshot.Version, "\x1b")
	assert.NotContains(t, snapshot.Version, "\a")
	assert.Equal(t, "running", snapshot.State)
	assert.Equal(t, "echo[31m.invalid", snapshot.Configuration.Target)
}

func TestClientRejectsUnboundedStatusCollections(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		mutate  func(*statusapi.Snapshot)
		wantErr string
	}{
		{
			name: "latency buckets",
			mutate: func(snapshot *statusapi.Snapshot) {
				snapshot.Client.TCPLatency.Buckets = make([]uint64, statusapi.LatencyBucketCount+1)
			},
			wantErr: "latency buckets",
		},
		{
			name: "traffic ports",
			mutate: func(snapshot *statusapi.Snapshot) {
				snapshot.Traffic.Ports = make([]metrics.PortSnapshot, config.MaxPorts+1)
			},
			wantErr: "port limit",
		},
		{
			name: "latency counters",
			mutate: func(snapshot *statusapi.Snapshot) {
				snapshot.Client.TCPLatency = statusapi.LatencySnapshot{Count: 1, SumNanos: 1, Buckets: make([]uint64, statusapi.LatencyBucketCount)}
			},
			wantErr: "inconsistent latency counters",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := statusapi.Snapshot{
				SchemaVersion: statusapi.SchemaVersion, Role: "client",
				SampledAt: now, StartedAt: now, Client: &statusapi.ClientSnapshot{},
			}
			test.mutate(&snapshot)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(snapshot)
			}))
			defer server.Close()
			client, err := NewClient(server.URL)
			require.NoError(t, err)
			_, err = client.Fetch()
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}
