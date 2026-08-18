// Package api exposes the QuorumForge ceremony service over HTTP, providing a
// JSON command API plus a minimal operations page for driving real ceremonies.
package api

import (
	"context"
	"net/http"
	"time"

	"quorumforge/internal/service"
)

// Server wraps the HTTP handler and lifecycle for the ceremony API.
type Server struct {
	svc    *service.Service
	server *http.Server
}

// New builds an HTTP server for the given service bound to addr.
func New(svc *service.Service, addr string) *Server {
	mux := http.NewServeMux()
	s := &Server{svc: svc}
	s.routes(mux)
	return &Server{
		svc: svc,
		server: &http.Server{
			Addr:              addr,
			Handler:           recoverMiddleware(logMiddleware(mux)),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
}

// routes registers every endpoint.
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /", s.handlePage)

	mux.HandleFunc("POST /api/v1/ceremonies", s.handleCreate)
	mux.HandleFunc("GET /api/v1/ceremonies/{id}", s.handleGet)

	mux.HandleFunc("POST /api/v1/ceremonies/{id}/lock", s.handleLock)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/witness", s.handleWitness)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/begin", s.handleBegin)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/receipt", s.handleReceipt)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/artifact", s.handleArtifact)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/review", s.handleReview)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/seal", s.handleSeal)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/quarantine", s.handleQuarantine)
	mux.HandleFunc("POST /api/v1/ceremonies/{id}/cancel", s.handleCancel)
}

// Handler exposes the raw handler for tests.
func (s *Server) Handler() http.Handler { return s.server.Handler }

// Serve starts listening and blocks until the server is shut down.
func (s *Server) Serve() error { return s.server.ListenAndServe() }

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.server.Addr }
