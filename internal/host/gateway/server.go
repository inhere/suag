package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gookit/rux/v2"
	"github.com/gookit/rux/v2/pkg/handlers"
)

// Server is the HTTP gateway for suag.
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
	r := rux.New(rux.WithMethodNotAllowed(true))
	r.Use(handlers.RequestLogger())
	r.Use(handlers.PanicsHandler())
	r.Use(corsMiddleware)

	// REST API.
	r.POST("/api/tasks", adaptHTTPHandler(s.handler.HandleCreateTask))
	r.GET("/api/hitl", adaptHTTPHandler(s.handler.HandleListHITL))
	r.POST("/api/hitl/{id}/respond", adaptHTTPHandler(s.handler.HandleHITLRespond))
	r.POST("/api/hitl/{id}/dismiss", adaptHTTPHandler(s.handler.HandleHITLDismiss))
	r.POST("/api/sessions", adaptHTTPHandler(s.handler.HandleCreateSession))
	r.GET("/api/sessions", adaptHTTPHandler(s.handler.HandleListSessions))
	r.GET("/api/sessions/{id}", adaptHTTPHandler(s.handler.HandleGetSession))
	r.GET("/api/sessions/{id}/runs/{runId}", adaptHTTPHandler(s.handler.HandleGetRunDetail))
	r.GET("/api/sessions/{id}/events", adaptHTTPHandler(s.handler.HandleListSessionEvents))
	r.GET("/api/sessions/{id}/memory-candidates", adaptHTTPHandler(s.handler.HandleListMemoryCandidates))
	r.POST("/api/sessions/{id}/turns", adaptHTTPHandler(s.handler.HandleStartTurn))
	r.POST("/api/memory-candidates/{id}/accept", adaptHTTPHandler(s.handler.HandleAcceptMemoryCandidate))
	r.POST("/api/memory-candidates/{id}/reject", adaptHTTPHandler(s.handler.HandleRejectMemoryCandidate))

	// Streaming endpoints.
	r.GET("/events", adaptHTTPHandler(SSEHandler(s.bus)))
	r.GET("/ws", adaptHTTPHandler(WSHandler(s.bus, s.handler.sessionRepo, s.handler.sessionSvc)))

	// Health check.
	r.GET("/health", adaptHTTPHandler(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok", "ts": time.Now().UTC().Format(time.RFC3339)})
	}))

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

	slog.Info("suag server starting", "addr", s.addr)

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

// adaptHTTPHandler preserves net/http PathValue for the existing handlers.
func adaptHTTPHandler(handler http.HandlerFunc) rux.HandlerFunc {
	return func(c *rux.Context) {
		for _, param := range c.Params().Snapshot() {
			c.Req.SetPathValue(param.Key, param.Value)
		}
		handler(c.Resp, c.Req)
	}
}

func corsMiddleware(c *rux.Context) {
	c.Resp.Header().Set("Access-Control-Allow-Origin", "*")
	c.Resp.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	c.Resp.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if c.Req.Method == http.MethodOptions {
		c.Resp.WriteHeader(http.StatusNoContent)
		c.Abort()
		return
	}
	c.Next()
}
