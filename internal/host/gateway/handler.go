package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/orchestrator"
)

// Handler exposes the REST API for the axon server.
type Handler struct {
	orch    *orchestrator.Orchestrator
	bus     *EventBus
	hitlMgr hitl.Manager
}

// NewHandler creates a Handler.
func NewHandler(orch *orchestrator.Orchestrator, bus *EventBus, hitlMgr hitl.Manager) *Handler {
	return &Handler{orch: orch, bus: bus, hitlMgr: hitlMgr}
}

// ─── POST /api/tasks ──────────────────────────────────────────────────────────

type createTaskRequest struct {
	Description string `json:"description"`
}

type createTaskResponse struct {
	TraceID string `json:"trace_id"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HandleCreateTask handles POST /api/tasks.
func (h *Handler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Description == "" {
		httpError(w, "description is required", http.StatusBadRequest)
		return
	}

	result, err := h.orch.RunTask(r.Context(), req.Description)
	if err != nil {
		slog.Error("run task error", "err", err)
		respondJSON(w, http.StatusInternalServerError, createTaskResponse{Error: err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, createTaskResponse{
		TraceID: result.TaskID,
		Output:  result.Output,
	})
}

// ─── GET /api/hitl ────────────────────────────────────────────────────────────

// HandleListHITL lists pending HITL tickets.
func (h *Handler) HandleListHITL(w http.ResponseWriter, r *http.Request) {
	tickets := h.hitlMgr.ListPending()
	respondJSON(w, http.StatusOK, tickets)
}

// ─── POST /api/hitl/{id}/respond ─────────────────────────────────────────────

type hitlRespondRequest struct {
	Instruction string `json:"instruction"`
}

// HandleHITLRespond handles POST /api/hitl/{id}/respond.
func (h *Handler) HandleHITLRespond(w http.ResponseWriter, r *http.Request) {
	ticketID := r.PathValue("id")
	if ticketID == "" {
		httpError(w, "ticket id required", http.StatusBadRequest)
		return
	}

	var req hitlRespondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := h.hitlMgr.Respond(ticketID, req.Instruction); err != nil {
		httpError(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── POST /api/hitl/{id}/dismiss ─────────────────────────────────────────────

// HandleHITLDismiss handles POST /api/hitl/{id}/dismiss.
func (h *Handler) HandleHITLDismiss(w http.ResponseWriter, r *http.Request) {
	ticketID := r.PathValue("id")
	if ticketID == "" {
		httpError(w, "ticket id required", http.StatusBadRequest)
		return
	}
	if err := h.hitlMgr.Dismiss(ticketID, "dismissed by operator"); err != nil {
		httpError(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, msg string, code int) {
	respondJSON(w, code, map[string]string{"error": msg})
}
