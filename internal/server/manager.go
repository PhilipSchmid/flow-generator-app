package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
)

type Server interface {
	Start() error
	Stop() error
	Port() int
	Type() string
}

// Manager manages multiple servers
type Manager struct {
	servers []Server
	mu      sync.Mutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewManager creates a new server manager
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		servers: make([]Server, 0),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// AddServer adds a server to the manager
func (m *Manager) AddServer(server Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers = append(m.servers, server)
}

// Start starts all servers
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("server manager is already running")
	}

	logging.Logger.Info("Starting server manager...")

	var errors []error
	for _, server := range m.servers {
		if err := server.Start(); err != nil {
			errors = append(errors, fmt.Errorf("failed to start %s server on port %d: %w",
				server.Type(), server.Port(), err))
		}
	}

	if len(errors) > 0 {
		// Stop any servers that started successfully
		for _, server := range m.servers {
			_ = server.Stop()
		}
		return fmt.Errorf("failed to start servers: %v", errors)
	}

	m.running = true
	logging.Logger.Info("All servers started successfully")
	return nil
}

// Stop stops all servers
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	logging.Logger.Info("Stopping server manager...")
	m.cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	for _, server := range m.servers {
		wg.Add(1)
		go func(s Server) {
			defer wg.Done()
			if err := s.Stop(); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("failed to stop %s server on port %d: %w",
					s.Type(), s.Port(), err))
				mu.Unlock()
			}
		}(server)
	}

	wg.Wait()
	m.running = false

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping servers: %v", errors)
	}

	logging.Logger.Info("All servers stopped successfully")
	return nil
}

// Wait blocks until the context is done
func (m *Manager) Wait() {
	<-m.ctx.Done()
}

// Running returns whether the manager is running
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// ServerCount returns the number of servers being managed
func (m *Manager) ServerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.servers)
}
