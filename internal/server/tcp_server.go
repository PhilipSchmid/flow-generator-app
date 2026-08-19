package server

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/handlers"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	statusmetrics "github.com/PhilipSchmid/flow-generator-app/internal/status"
)

const (
	networkFailureLogInterval = 30 * time.Second
	acceptRetryMinDelay       = 5 * time.Millisecond
	acceptRetryMaxDelay       = time.Second
)

// TCPServer represents a TCP server
type TCPServer struct {
	port          int
	listener      net.Listener
	handler       *handlers.TCPHandler
	statusTracker *statusmetrics.ServerTracker
	acceptLogs    *logging.RateLimiter
	onFailure     func(error)
	wg            sync.WaitGroup
	connsMu       sync.Mutex
	conns         map[net.Conn]struct{}
	stopping      bool
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewTCPServer creates a new TCP server
func NewTCPServer(port int, handler *handlers.TCPHandler) *TCPServer {
	return newTCPServer(port, handler, nil)
}

// NewTCPServerWithStatus creates a TCP server with dashboard error counters.
func NewTCPServerWithStatus(port int, handler *handlers.TCPHandler, tracker *statusmetrics.ServerTracker) *TCPServer {
	return newTCPServer(port, handler, tracker)
}

func newTCPServer(port int, handler *handlers.TCPHandler, tracker *statusmetrics.ServerTracker) *TCPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &TCPServer{
		port:          port,
		handler:       handler,
		statusTracker: tracker,
		acceptLogs:    logging.NewRateLimiter(networkFailureLogInterval),
		conns:         make(map[net.Conn]struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// SetFailureHandler registers a callback for a permanent listener failure.
// It must be called before Start.
func (s *TCPServer) SetFailureHandler(handler func(error)) {
	s.onFailure = handler
}

// Start starts the TCP server
func (s *TCPServer) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on TCP port %d: %w", s.port, err)
	}
	s.listener = listener
	s.port = listener.Addr().(*net.TCPAddr).Port

	logging.Logger.Infof("TCP server listening on port %d", s.port)

	s.wg.Add(1)
	go s.acceptConnections()

	return nil
}

// Stop stops the TCP server
func (s *TCPServer) Stop() error {
	s.cancel()
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			logging.Logger.Warnf("Error closing TCP listener on port %d: %v", s.port, err)
		}
	}
	s.connsMu.Lock()
	s.stopping = true
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.connsMu.Unlock()
	s.wg.Wait()
	logging.Logger.Infof("TCP server on port %d stopped", s.port)
	return nil
}

// acceptConnections accepts incoming connections
func (s *TCPServer) acceptConnections() {
	defer s.wg.Done()

	var retryDelay time.Duration
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				if s.statusTracker != nil {
					s.statusTracker.RecordAcceptError()
				}
				s.acceptLogs.Errorw("TCP connection accept failed", "port", s.port, "error", err)
				temporary, ok := err.(interface{ Temporary() bool })
				if !ok || !temporary.Temporary() {
					if s.onFailure != nil {
						s.onFailure(err)
					}
					return
				}
				if retryDelay == 0 {
					retryDelay = acceptRetryMinDelay
				} else {
					retryDelay = min(retryDelay*2, acceptRetryMaxDelay)
				}
				timer := time.NewTimer(retryDelay)
				select {
				case <-timer.C:
				case <-s.ctx.Done():
					timer.Stop()
					return
				}
				continue
			}
		}
		retryDelay = 0

		s.connsMu.Lock()
		if s.stopping {
			s.connsMu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()
		s.wg.Add(1)
		go func() {
			defer func() {
				s.connsMu.Lock()
				delete(s.conns, conn)
				s.connsMu.Unlock()
				s.wg.Done()
			}()
			s.handler.Handle(conn)
		}()
	}
}

// Port returns the server port
func (s *TCPServer) Port() int {
	return s.port
}

// Type returns the server type
func (s *TCPServer) Type() string {
	return "TCP"
}
