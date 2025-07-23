package health

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
)

type Checker struct {
	ready   atomic.Bool
	healthy atomic.Bool
	server  *http.Server
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

	go func() {
		if logging.Logger != nil {
			logging.Logger.Infof("Health check server starting on port %s", port)
		}
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if logging.Logger != nil {
				logging.Logger.Errorf("Health check server error: %v", err)
			}
		}
	}()

	// Mark as healthy immediately after starting
	c.SetHealthy(true)

	return nil
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
