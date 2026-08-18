package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
)

const Path = "/api/v1/status"

// Provider returns a current immutable status snapshot.
type Provider func() Snapshot

// Server owns the loopback-only status listener.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

// Start binds a local status endpoint. Port "0" disables the endpoint.
func Start(port string, provider Provider) (*Server, error) {
	if port == "0" {
		return nil, nil
	}
	return startAt(net.JoinHostPort("127.0.0.1", port), provider)
}

func startAt(address string, provider Provider) (*Server, error) {
	if provider == nil {
		return nil, errors.New("status provider is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc(Path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewEncoder(w).Encode(provider()); err != nil {
			logging.Logger.Warnf("Failed to encode local status response: %v", err)
		}
	})

	httpServer := &http.Server{
		Addr: address, Handler: mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      2 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on local status address %s: %w", address, err)
	}
	server := &Server{httpServer: httpServer, listener: listener}
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Logger.Errorf("Local status server stopped unexpectedly: %v", err)
		}
	}()
	logging.Logger.Infof("Local dashboard status available on %s%s", listener.Addr(), Path)
	return server, nil
}

func (s *Server) Addr() net.Addr {
	if s == nil || s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) Stop(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown local status server: %w", err)
	}
	return nil
}
