package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server owns the Prometheus HTTP listener and its shutdown lifecycle.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

// StartMetricsServer binds the configured port before returning, so startup
// failures are reported to the caller rather than being lost in a goroutine.
func StartMetricsServer(port string) (*Server, error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen on metrics port %s: %w", port, err)
	}
	server := &Server{httpServer: httpServer, listener: listener}

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Logger.Errorf("Metrics server stopped unexpectedly: %v", err)
		}
	}()
	logging.Logger.Infof("Prometheus metrics available on %s", listener.Addr())
	return server, nil
}

// Addr returns the bound metrics listener address.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Stop gracefully stops the metrics server.
func (s *Server) Stop(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown metrics server: %w", err)
	}
	return nil
}
