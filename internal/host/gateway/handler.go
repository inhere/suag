package gateway

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/orchestrator"
)

// Handler exposes the REST API for the axon server.
type Handler struct {
	orch         *orchestrator.Orchestrator
	sessionStore session.Store
	sessionSvc   *session.Service
	bus          *EventBus
	hitlMgr      hitl.Manager
}

// NewHandler creates a Handler.
func NewHandler(orch *orchestrator.Orchestrator, sessionStore session.Store, sessionSvc *session.Service, bus *EventBus, hitlMgr hitl.Manager) *Handler {
	return &Handler{
		orch:         orch,
		sessionStore: sessionStore,
		sessionSvc:   sessionSvc,
		bus:          bus,
		hitlMgr:      hitlMgr,
	}
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
	if h.orch == nil {
		httpError(w, "task service is not configured", http.StatusServiceUnavailable)
		return
	}

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

// ─── POST /api/sessions ──────────────────────────────────────────────────────

type createSessionRequest struct {
	Title string `json:"title"`
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}

func (h *Handler) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	if h.sessionStore == nil {
		httpError(w, "session store is not configured", http.StatusServiceUnavailable)
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) && err.Error() != "EOF" {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	sessionModel := session.Session{
		SessionID: session.NewSessionID(),
		Title:     strings.TrimSpace(req.Title),
		Status:    session.SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}
	if sessionModel.Title == "" {
		sessionModel.Title = "Session " + sessionModel.SessionID
	}

	if err := h.sessionStore.CreateSession(r.Context(), sessionModel); err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusCreated, createSessionResponse{
		SessionID: sessionModel.SessionID,
		Title:     sessionModel.Title,
		Status:    string(sessionModel.Status),
	})
}

// ─── POST /api/sessions/{id}/turns ───────────────────────────────────────────

type startTurnRequest struct {
	AgentID       string `json:"agent_id"`
	Message       string `json:"message"`
	InitiatorType string `json:"initiator_type"`
}

type startTurnResponse struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	RunID     string `json:"run_id"`
}

func (h *Handler) HandleStartTurn(w http.ResponseWriter, r *http.Request) {
	if h.sessionSvc == nil {
		httpError(w, "session service is not configured", http.StatusServiceUnavailable)
		return
	}

	sessionID := r.PathValue("id")
	if strings.TrimSpace(sessionID) == "" {
		httpError(w, "session id required", http.StatusBadRequest)
		return
	}

	var req startTurnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.sessionSvc.StartTurn(r.Context(), session.StartTurnInput{
		SessionID:     sessionID,
		AgentID:       req.AgentID,
		UserMessage:   req.Message,
		InitiatorType: session.InitiatorType(req.InitiatorType),
	})
	if err != nil {
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, http.StatusOK, startTurnResponse(result))
}

// ─── GET /api/sessions/{id} ──────────────────────────────────────────────────

type getSessionResponse struct {
	SessionID     string                 `json:"session_id"`
	Title         string                 `json:"title"`
	Status        string                 `json:"status"`
	LastTurnID    string                 `json:"last_turn_id"`
	ExternalTasks []externalTaskResponse `json:"external_tasks"`
}

type externalTaskResponse struct {
	ExternalTaskID  string         `json:"external_task_id"`
	SessionID       string         `json:"session_id"`
	TurnID          string         `json:"turn_id"`
	RunID           string         `json:"run_id"`
	Adapter         string         `json:"adapter"`
	RemoteAgentName string         `json:"remote_agent_name"`
	RemoteSessionID string         `json:"remote_session_id"`
	RemoteTaskID    string         `json:"remote_task_id"`
	State           string         `json:"state"`
	LastHeartbeatAt string         `json:"last_heartbeat_at"`
	ResultSummary   string         `json:"result_summary"`
	Metadata        map[string]any `json:"metadata"`
}

func (h *Handler) HandleGetSession(w http.ResponseWriter, r *http.Request) {
	if h.sessionStore == nil {
		httpError(w, "session store is not configured", http.StatusServiceUnavailable)
		return
	}

	sessionID := r.PathValue("id")
	sessionModel, err := h.sessionStore.GetSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			httpError(w, "session not found", http.StatusNotFound)
			return
		}
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	externalTasks, err := h.sessionStore.ListExternalTasksBySession(r.Context(), sessionID)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, getSessionResponse{
		SessionID:     sessionModel.SessionID,
		Title:         sessionModel.Title,
		Status:        string(sessionModel.Status),
		LastTurnID:    sessionModel.LastTurnID,
		ExternalTasks: mapExternalTaskResponses(externalTasks),
	})
}

// ─── GET /api/sessions/{id}/events ───────────────────────────────────────────

func (h *Handler) HandleListSessionEvents(w http.ResponseWriter, r *http.Request) {
	if h.sessionStore == nil {
		httpError(w, "session store is not configured", http.StatusServiceUnavailable)
		return
	}

	sessionID := r.PathValue("id")
	afterSequence := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after_seq")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			httpError(w, "invalid after_seq", http.StatusBadRequest)
			return
		}
		afterSequence = value
	}

	events, err := h.sessionStore.ListEventsBySession(r.Context(), sessionID, afterSequence)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := make([]Event, 0, len(events))
	for _, event := range events {
		resp = append(resp, Event{
			Type:      event.EventType,
			SessionID: event.SessionID,
			TurnID:    event.TurnID,
			RunID:     event.RunID,
			AgentID:   event.AgentID,
			Sequence:  event.Sequence,
			Payload:   event.Payload,
		})
	}
	respondJSON(w, http.StatusOK, resp)
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

func mapExternalTaskResponses(tasks []session.ExternalTask) []externalTaskResponse {
	resp := make([]externalTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		resp = append(resp, externalTaskResponse{
			ExternalTaskID:  task.ExternalTaskID,
			SessionID:       task.SessionID,
			TurnID:          task.TurnID,
			RunID:           task.RunID,
			Adapter:         task.Adapter,
			RemoteAgentName: task.RemoteAgentName,
			RemoteSessionID: task.RemoteSessionID,
			RemoteTaskID:    task.RemoteTaskID,
			State:           task.State,
			LastHeartbeatAt: task.LastHeartbeatAt.UTC().Format(time.RFC3339Nano),
			ResultSummary:   task.ResultSummary,
			Metadata:        task.Metadata,
		})
	}
	return resp
}
