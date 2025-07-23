package server

import (
	"errors"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Initialize logger for tests
	logging.InitLogger("json", "error")
}

// mockServer implements the Server interface for testing
type mockServer struct {
	port     int
	typ      string
	started  bool
	stopped  bool
	startErr error
	stopErr  error
}

func (m *mockServer) Start() error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	return nil
}

func (m *mockServer) Stop() error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stopped = true
	return nil
}

func (m *mockServer) Port() int {
	return m.port
}

func (m *mockServer) Type() string {
	return m.typ
}

func TestNewManager(t *testing.T) {
	manager := NewManager()

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.servers)
	assert.Equal(t, 0, len(manager.servers))
	assert.False(t, manager.running)
	assert.NotNil(t, manager.ctx)
	assert.NotNil(t, manager.cancel)
}

func TestManagerAddServer(t *testing.T) {
	manager := NewManager()

	server1 := &mockServer{port: 8080, typ: "TCP"}
	server2 := &mockServer{port: 9000, typ: "UDP"}

	manager.AddServer(server1)
	assert.Equal(t, 1, manager.ServerCount())

	manager.AddServer(server2)
	assert.Equal(t, 2, manager.ServerCount())
}

func TestManagerStart(t *testing.T) {
	tests := []struct {
		name    string
		servers []*mockServer
		wantErr bool
		errMsg  string
	}{
		{
			name: "successful start",
			servers: []*mockServer{
				{port: 8080, typ: "TCP"},
				{port: 9000, typ: "UDP"},
			},
			wantErr: false,
		},
		{
			name: "one server fails to start",
			servers: []*mockServer{
				{port: 8080, typ: "TCP"},
				{port: 9000, typ: "UDP", startErr: errors.New("bind failed")},
			},
			wantErr: true,
			errMsg:  "failed to start servers",
		},
		{
			name:    "no servers",
			servers: []*mockServer{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager()

			for _, server := range tt.servers {
				manager.AddServer(server)
			}

			err := manager.Start()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				// Verify all servers were stopped on error
				for _, server := range tt.servers {
					if server.started && server.startErr == nil {
						assert.True(t, server.stopped)
					}
				}
			} else {
				assert.NoError(t, err)
				assert.True(t, manager.Running())
				// Verify all servers were started
				for _, server := range tt.servers {
					assert.True(t, server.started)
				}
			}
		})
	}
}

func TestManagerStartAlreadyRunning(t *testing.T) {
	manager := NewManager()
	server := &mockServer{port: 8080, typ: "TCP"}
	manager.AddServer(server)

	// Start manager
	err := manager.Start()
	require.NoError(t, err)

	// Try to start again
	err = manager.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestManagerStop(t *testing.T) {
	manager := NewManager()

	server1 := &mockServer{port: 8080, typ: "TCP"}
	server2 := &mockServer{port: 9000, typ: "UDP"}

	manager.AddServer(server1)
	manager.AddServer(server2)

	// Start manager
	err := manager.Start()
	require.NoError(t, err)

	// Stop manager
	err = manager.Stop()
	assert.NoError(t, err)
	assert.False(t, manager.Running())

	// Verify all servers were stopped
	assert.True(t, server1.stopped)
	assert.True(t, server2.stopped)
}

func TestManagerStopNotRunning(t *testing.T) {
	manager := NewManager()

	// Stop without starting
	err := manager.Stop()
	assert.NoError(t, err)
}

func TestManagerStopWithErrors(t *testing.T) {
	manager := NewManager()

	server1 := &mockServer{port: 8080, typ: "TCP"}
	server2 := &mockServer{port: 9000, typ: "UDP", stopErr: errors.New("stop failed")}

	manager.AddServer(server1)
	manager.AddServer(server2)

	// Start manager
	err := manager.Start()
	require.NoError(t, err)

	// Stop manager
	err = manager.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "errors stopping servers")

	// Verify server1 was still stopped
	assert.True(t, server1.stopped)
}

func TestManagerWait(t *testing.T) {
	manager := NewManager()

	// Start wait in goroutine
	waitDone := make(chan bool)
	go func() {
		manager.Wait()
		waitDone <- true
	}()

	// Cancel context
	manager.cancel()

	// Wait should return
	select {
	case <-waitDone:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Wait did not return after context cancel")
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	manager := NewManager()

	// Add servers concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(port int) {
			server := &mockServer{port: port, typ: "TCP"}
			manager.AddServer(server)
			done <- true
		}(8080 + i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 10, manager.ServerCount())
}

func BenchmarkManagerAddServer(b *testing.B) {
	manager := NewManager()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		server := &mockServer{port: 8080 + i, typ: "TCP"}
		manager.AddServer(server)
	}
}

func BenchmarkManagerStartStop(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		manager := NewManager()
		server := &mockServer{port: 8080, typ: "TCP"}
		manager.AddServer(server)

		_ = manager.Start()
		_ = manager.Stop()
	}
}
