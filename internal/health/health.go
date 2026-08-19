package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
)

type Checker struct {
	ready    atomic.Bool
	healthy  atomic.Bool
	server   *http.Server
	listener net.Listener
}

// NewChecker creates a new health checker
func NewChecker() *Checker {
	return &Checker{}
}

func (c *Checker) SetReady(ready bool) {
	c.ready.Store(ready)
}

func (c *Checker) SetHealthy(healthy bool) {
	c.healthy.Store(healthy)
}

// Ready reports whether the server is ready to accept traffic.
func (c *Checker) Ready() bool {
	return c.ready.Load()
}

// Healthy reports whether the health listener is running normally.
func (c *Checker) Healthy() bool {
	return c.healthy.Load()
}

// Start starts the health check server on the specified port
func (c *Checker) Start(port string) error {
	mux := http.NewServeMux()

	// Health endpoint - basic liveness check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if c.healthy.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Unhealthy"))
		}
	})

	// Ready endpoint - readiness check
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if c.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Not Ready"))
		}
	})

	c.server = &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", c.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on health port %s: %w", port, err)
	}
	c.listener = listener
	c.SetHealthy(true)

	go func() {
		if err := c.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.SetHealthy(false)
			c.SetReady(false)
			logging.Logger.Errorf("Health check server stopped unexpectedly: %v", err)
		}
	}()
	logging.Logger.Infof("Health checks available on %s", listener.Addr())

	return nil
}

// Addr returns the bound health listener address.
func (c *Checker) Addr() net.Addr {
	if c.listener == nil {
		return nil
	}
	return c.listener.Addr()
}

// Stop gracefully stops the health check server
func (c *Checker) Stop() error {
	if c.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c.SetHealthy(false)
	c.SetReady(false)

	if err := c.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("health check server shutdown error: %w", err)
	}

	return nil
}
