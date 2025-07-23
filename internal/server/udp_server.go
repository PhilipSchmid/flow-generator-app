package server

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/PhilipSchmid/flow-generator-app/internal/handlers"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
)

// UDPServer represents a UDP server
type UDPServer struct {
	port    int
	conn    *net.UDPConn
	handler *handlers.UDPHandler
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewUDPServer creates a new UDP server
func NewUDPServer(port int, handler *handlers.UDPHandler) *UDPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &UDPServer{
		port:    port,
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start starts the UDP server
func (s *UDPServer) Start() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", s.port, err)
	}
	s.conn = conn

	logging.Logger.Infof("UDP server listening on port %d", s.port)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handler.Handle(s.conn)
	}()

	return nil
}

// Stop stops the UDP server
func (s *UDPServer) Stop() error {
	s.cancel()
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			logging.Logger.Warnf("Error closing UDP connection on port %d: %v", s.port, err)
		}
	}
	s.wg.Wait()
	logging.Logger.Infof("UDP server on port %d stopped", s.port)
	return nil
}

// Port returns the server port
func (s *UDPServer) Port() int {
	return s.port
}

// Type returns the server type
func (s *UDPServer) Type() string {
	return "UDP"
}
