package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the HTTP gateway for axon.
type Server struct {
	handler *Handler
	bus     *EventBus
	addr    string
	srv     *http.Server
}

// NewServer creates a Server bound to addr.
func NewServer(addr string, handler *Handler, bus *EventBus) *Server {
	return &Server{addr: addr, handler: handler, bus: bus}
}

func (s *Server) router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// REST API.
	r.Post("/api/tasks", s.handler.HandleCreateTask)
	r.Get("/api/hitl", s.handler.HandleListHITL)
	r.Post("/api/hitl/{id}/respond", s.handler.HandleHITLRespond)
	r.Post("/api/hitl/{id}/dismiss", s.handler.HandleHITLDismiss)
	r.Post("/api/sessions", s.handler.HandleCreateSession)
	r.Get("/api/sessions/{id}", s.handler.HandleGetSession)
	r.Get("/api/sessions/{id}/runs/{runId}", s.handler.HandleGetRunDetail)
	r.Get("/api/sessions/{id}/events", s.handler.HandleListSessionEvents)
	r.Post("/api/sessions/{id}/turns", s.handler.HandleStartTurn)

	// Streaming endpoints.
	r.Get("/events", SSEHandler(s.bus))
	r.Get("/ws", WSHandler(s.bus, s.handler.sessionStore, s.handler.sessionSvc))

	// Health check.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "ts": time.Now().UTC().Format(time.RFC3339)})
	})

	return r
}

// Start builds the router and starts listening. Blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:         s.addr,
		Handler:      s.router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE streams need unlimited write time
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("axon server starting", "addr", s.addr)

	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutCtx)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
