package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/inhere/axon/internal/host/agui"
	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/orchestrator"
)

// Handler exposes the REST API for the axon server.
type Handler struct {
	orch         *orchestrator.Orchestrator
	sessionStore session.Repository
	sessionSvc   *session.Service
	bus          *EventBus
	hitlMgr      hitl.Manager
}

// NewHandler creates a Handler.
func NewHandler(orch *orchestrator.Orchestrator, sessionStore session.Repository, sessionSvc *session.Service, bus *EventBus, hitlMgr hitl.Manager) *Handler {
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

type listSessionsResponseItem struct {
	SessionID  string `json:"session_id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	LastTurnID string `json:"last_turn_id"`
	UpdatedAt  string `json:"updated_at"`
	Preview    string `json:"preview"`
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

func (h *Handler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	if h.sessionStore == nil {
		httpError(w, "session store is not configured", http.StatusServiceUnavailable)
		return
	}

	sessions, err := h.sessionStore.ListSessions(r.Context())
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := make([]listSessionsResponseItem, 0, len(sessions))
	for _, sessionModel := range sessions {
		preview, err := h.buildSessionPreview(r, sessionModel)
		if err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp = append(resp, listSessionsResponseItem{
			SessionID:  sessionModel.SessionID,
			Title:      sessionModel.Title,
			Status:     string(sessionModel.Status),
			LastTurnID: sessionModel.LastTurnID,
			UpdatedAt:  sessionModel.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Preview:    preview,
		})
	}

	respondJSON(w, http.StatusOK, resp)
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
	SessionID          string                 `json:"session_id"`
	Title              string                 `json:"title"`
	Status             string                 `json:"status"`
	LastTurnID         string                 `json:"last_turn_id"`
	Turns              []map[string]any       `json:"turns"`
	Runs               []map[string]any       `json:"runs"`
	ExternalActivities []map[string]any       `json:"external_activities"`
	ExternalMessages   []map[string]any       `json:"external_messages"`
	ExternalTasks      []externalTaskResponse `json:"external_tasks"`
}

type getRunDetailResponse struct {
	SessionID          string                 `json:"session_id"`
	TurnID             string                 `json:"turn_id"`
	Turn               map[string]any         `json:"turn"`
	Run                map[string]any         `json:"run"`
	ExternalActivities []map[string]any       `json:"external_activities"`
	ExternalMessages   []map[string]any       `json:"external_messages"`
	ExternalTasks      []externalTaskResponse `json:"external_tasks"`
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

type memoryCandidateResponse struct {
	CandidateID   string         `json:"candidate_id"`
	SessionID     string         `json:"session_id"`
	TurnID        string         `json:"turn_id"`
	RunID         string         `json:"run_id"`
	SourceRecapID string         `json:"source_recap_id"`
	Target        string         `json:"target"`
	Kind          string         `json:"kind"`
	Content       string         `json:"content"`
	Score         float64        `json:"score"`
	Status        string         `json:"status"`
	CreatedAt     string         `json:"created_at"`
	Metadata      map[string]any `json:"metadata"`
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
	runEvents, err := h.sessionStore.ListEventsBySession(r.Context(), sessionID, 0)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snapshot := agui.BuildStateSnapshot(sessionModel, mapSessionEventsForSnapshot(h.sessionStore, r.Context(), runEvents), externalTasks)
	snapshotPayload, _ := snapshot.Snapshot.(map[string]any)

	respondJSON(w, http.StatusOK, getSessionResponse{
		SessionID:          sessionModel.SessionID,
		Title:              sessionModel.Title,
		Status:             string(sessionModel.Status),
		LastTurnID:         sessionModel.LastTurnID,
		Turns:              snapshotSliceField(snapshotPayload, "turns"),
		Runs:               snapshotSliceField(snapshotPayload, "runs"),
		ExternalActivities: snapshotSliceField(snapshotPayload, "externalActivities"),
		ExternalMessages:   snapshotSliceField(snapshotPayload, "externalMessages"),
		ExternalTasks:      mapExternalTaskResponses(externalTasks),
	})
}

func (h *Handler) HandleGetRunDetail(w http.ResponseWriter, r *http.Request) {
	if h.sessionStore == nil {
		httpError(w, "session store is not configured", http.StatusServiceUnavailable)
		return
	}

	sessionID := r.PathValue("id")
	runID := r.PathValue("runId")
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(runID) == "" {
		httpError(w, "session id and run id are required", http.StatusBadRequest)
		return
	}

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
	runEvents, err := h.sessionStore.ListEventsBySession(r.Context(), sessionID, 0)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	snapshot := agui.BuildStateSnapshot(sessionModel, mapSessionEventsForSnapshot(h.sessionStore, r.Context(), runEvents), externalTasks)
	snapshotPayload, _ := snapshot.Snapshot.(map[string]any)
	runs := snapshotSliceField(snapshotPayload, "runs")
	run := findRunSnapshot(runs, runID)
	if run == nil {
		httpError(w, "run not found", http.StatusNotFound)
		return
	}

	turnID, _ := run["turnId"].(string)
	turn := findTurnSnapshot(snapshotSliceField(snapshotPayload, "turns"), turnID)
	filteredActivities := filterSnapshotByRunID(snapshotSliceField(snapshotPayload, "externalActivities"), runID)
	filteredMessages := filterSnapshotByRunID(snapshotSliceField(snapshotPayload, "externalMessages"), runID)
	filteredTasks := filterExternalTaskResponsesByRunID(mapExternalTaskResponses(externalTasks), runID)

	respondJSON(w, http.StatusOK, getRunDetailResponse{
		SessionID:          sessionID,
		TurnID:             turnID,
		Turn:               turn,
		Run:                run,
		ExternalActivities: filteredActivities,
		ExternalMessages:   filteredMessages,
		ExternalTasks:      filteredTasks,
	})
}

func (h *Handler) HandleListMemoryCandidates(w http.ResponseWriter, r *http.Request) {
	if h.sessionStore == nil {
		httpError(w, "session store is not configured", http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimSpace(r.PathValue("id"))
	if sessionID == "" {
		httpError(w, "session id required", http.StatusBadRequest)
		return
	}

	candidates, err := h.sessionStore.ListMemoryCandidatesBySession(r.Context(), sessionID)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, mapMemoryCandidateResponses(candidates))
}

func (h *Handler) HandleAcceptMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	h.handleUpdateMemoryCandidate(w, r, session.MemoryCandidateApplied)
}

func (h *Handler) HandleRejectMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	h.handleUpdateMemoryCandidate(w, r, session.MemoryCandidateRejected)
}

func (h *Handler) handleUpdateMemoryCandidate(w http.ResponseWriter, r *http.Request, status session.MemoryCandidateStatus) {
	if h.sessionSvc == nil {
		httpError(w, "session service is not configured", http.StatusServiceUnavailable)
		return
	}

	candidateID := strings.TrimSpace(r.PathValue("id"))
	if candidateID == "" {
		httpError(w, "candidate id required", http.StatusBadRequest)
		return
	}

	candidate, err := h.sessionSvc.UpdateMemoryCandidate(r.Context(), session.UpdateMemoryCandidateInput{
		CandidateID: candidateID,
		Status:      status,
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httpError(w, "memory candidate not found", http.StatusNotFound)
			return
		}
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, http.StatusOK, mapMemoryCandidateResponse(candidate))
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

func mapMemoryCandidateResponses(candidates []session.MemoryCandidate) []memoryCandidateResponse {
	resp := make([]memoryCandidateResponse, 0, len(candidates))
	for _, candidate := range candidates {
		resp = append(resp, mapMemoryCandidateResponse(candidate))
	}
	return resp
}

func mapMemoryCandidateResponse(candidate session.MemoryCandidate) memoryCandidateResponse {
	return memoryCandidateResponse{
		CandidateID:   candidate.CandidateID,
		SessionID:     candidate.SessionID,
		TurnID:        candidate.TurnID,
		RunID:         candidate.RunID,
		SourceRecapID: candidate.SourceRecapID,
		Target:        candidate.Target,
		Kind:          candidate.Kind,
		Content:       candidate.Content,
		Score:         candidate.Score,
		Status:        string(candidate.Status),
		CreatedAt:     candidate.CreatedAt.UTC().Format(time.RFC3339Nano),
		Metadata:      candidate.Metadata,
	}
}

func mapSessionEventsForSnapshot(store interface {
	GetRun(ctx context.Context, runID string) (session.Run, error)
}, ctx context.Context, events []session.RunEvent) []agui.HostEvent {
	runMetadataByRunID := make(map[string]map[string]any)
	if store != nil {
		for _, event := range events {
			if strings.TrimSpace(event.RunID) == "" {
				continue
			}
			if _, ok := runMetadataByRunID[event.RunID]; ok {
				continue
			}
			runModel, err := store.GetRun(ctx, event.RunID)
			if err != nil || len(runModel.Metadata) == 0 {
				continue
			}
			runMetadataByRunID[event.RunID] = cloneSnapshotMap(runModel.Metadata)
		}
	}

	out := make([]agui.HostEvent, 0, len(events))
	for _, event := range events {
		out = append(out, agui.HostEvent{
			Type:        event.EventType,
			SessionID:   event.SessionID,
			TurnID:      event.TurnID,
			RunID:       event.RunID,
			AgentID:     event.AgentID,
			Sequence:    event.Sequence,
			Payload:     event.Payload,
			RunMetadata: cloneSnapshotMap(runMetadataByRunID[event.RunID]),
			CreatedAt:   event.CreatedAt,
		})
	}
	return out
}

func snapshotSliceField(snapshot map[string]any, key string) []map[string]any {
	if snapshot == nil {
		return []map[string]any{}
	}
	values, ok := snapshot[key].([]map[string]any)
	if !ok {
		return []map[string]any{}
	}
	return values
}

func findRunSnapshot(runs []map[string]any, runID string) map[string]any {
	for _, run := range runs {
		if run["runId"] == runID {
			return run
		}
	}
	return nil
}

func findTurnSnapshot(turns []map[string]any, turnID string) map[string]any {
	for _, turn := range turns {
		if turn["turnId"] == turnID {
			return turn
		}
	}
	return nil
}

func filterSnapshotByRunID(items []map[string]any, runID string) []map[string]any {
	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item["runId"] == runID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterExternalTaskResponsesByRunID(tasks []externalTaskResponse, runID string) []externalTaskResponse {
	filtered := make([]externalTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		if task.RunID == runID {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func cloneSnapshotMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func (h *Handler) buildSessionPreview(r *http.Request, sessionModel session.Session) (string, error) {
	externalTasks, err := h.sessionStore.ListExternalTasksBySession(r.Context(), sessionModel.SessionID)
	if err != nil {
		return "", err
	}
	runEvents, err := h.sessionStore.ListEventsBySession(r.Context(), sessionModel.SessionID, 0)
	if err != nil {
		return "", err
	}

	snapshot := agui.BuildStateSnapshot(sessionModel, mapSessionEventsForSnapshot(h.sessionStore, r.Context(), runEvents), externalTasks)
	snapshotPayload, _ := snapshot.Snapshot.(map[string]any)
	return snapshotPreview(snapshotPayload), nil
}

func snapshotPreview(snapshot map[string]any) string {
	if snapshot == nil {
		return ""
	}

	turns := snapshotSliceField(snapshot, "turns")
	turnID, _ := snapshot["turnId"].(string)
	lastTurnID, _ := snapshot["lastTurnId"].(string)
	currentTurn := findTurnSnapshot(turns, turnID)
	if currentTurn == nil {
		currentTurn = findTurnSnapshot(turns, lastTurnID)
	}
	if currentTurn == nil && len(turns) > 0 {
		currentTurn = turns[0]
	}
	if currentTurn != nil {
		if preview, _ := currentTurn["latestMessagePreview"].(string); strings.TrimSpace(preview) != "" {
			return preview
		}
	}

	runs := snapshotSliceField(snapshot, "runs")
	currentRunID, _ := snapshot["currentRunId"].(string)
	currentRun := findRunSnapshot(runs, currentRunID)
	if currentRun == nil && len(runs) > 0 {
		currentRun = runs[0]
	}
	if currentRun == nil {
		return ""
	}
	if summary, _ := currentRun["summary"].(string); strings.TrimSpace(summary) != "" {
		return summary
	}
	if latestMessage, _ := currentRun["latestMessageText"].(string); strings.TrimSpace(latestMessage) != "" {
		return latestMessage
	}
	return ""
}
