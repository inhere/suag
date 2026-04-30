package session

import "time"

type SessionStatus string
type TurnStatus string
type RunState string
type InitiatorType string
type MemoryCandidateStatus string

// LoopPhase tracks whether a host run is the first execution attempt or a
// resume of a previously waiting turn.
type LoopPhase string

// ContinueMode records which host-side continue path produced the run.
type ContinueMode string

// TurnOutcome captures the final host outcome written after a run reaches a
// terminal lifecycle state.
type TurnOutcome string

// PlannerHint is a lightweight planning artifact for post-turn follow-up.
type PlannerHint struct {
	Stage    string
	Summary  string
	Metadata map[string]any
}

// NextActionHint describes the most relevant host-facing next step after a
// terminal run completes.
type NextActionHint struct {
	Kind     string
	Title    string
	Message  string
	Metadata map[string]any
}

const (
	SessionOpen     SessionStatus = "OPEN"
	SessionRunning  SessionStatus = "RUNNING"
	SessionWaiting  SessionStatus = "WAITING"
	SessionArchived SessionStatus = "ARCHIVED"
)

const (
	TurnPending   TurnStatus = "PENDING"
	TurnRunning   TurnStatus = "RUNNING"
	TurnWaiting   TurnStatus = "WAITING"
	TurnCanceled  TurnStatus = "CANCELED"
	TurnCompleted TurnStatus = "COMPLETED"
	TurnFailed    TurnStatus = "FAILED"
)

const (
	RunQueued        RunState = "QUEUED"
	RunPreparing     RunState = "PREPARING"
	RunThinking      RunState = "THINKING"
	RunDispatching   RunState = "DISPATCHING"
	RunExternal      RunState = "EXTERNAL_RUNNING"
	RunInputRequired RunState = "INPUT_REQUIRED"
	RunAuthRequired  RunState = "AUTH_REQUIRED"
	RunSummarizing   RunState = "SUMMARIZING"
	RunCompleted     RunState = "COMPLETED"
	RunFailed        RunState = "FAILED"
	RunCanceled      RunState = "CANCELED"
)

const (
	InitiatorUser   InitiatorType = "user"
	InitiatorSystem InitiatorType = "system"
	InitiatorAgent  InitiatorType = "agent"
)

const (
	MemoryCandidateDraft    MemoryCandidateStatus = "draft"
	MemoryCandidateRendered MemoryCandidateStatus = "rendered"
	MemoryCandidateApplied  MemoryCandidateStatus = "applied"
	MemoryCandidateRejected MemoryCandidateStatus = "rejected"
)

const (
	LoopPhaseInitial LoopPhase = "initial"
	LoopPhaseResumed LoopPhase = "resumed"
)

const (
	ContinueModeStart   ContinueMode = "start"
	ContinueModeMessage ContinueMode = "message"
)

const (
	TurnOutcomeCompleted TurnOutcome = "completed"
	TurnOutcomeFailed    TurnOutcome = "failed"
	TurnOutcomeCanceled  TurnOutcome = "canceled"
)

type Session struct {
	SessionID  string
	Title      string
	Status     SessionStatus
	LastTurnID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Metadata   map[string]any
}

type Turn struct {
	TurnID         string
	SessionID      string
	InitiatorType  InitiatorType
	InitiatorID    string
	InputMessageID string
	InputSummary   string
	Status         TurnStatus
	PrimaryRunID   string
	CreatedAt      time.Time
	CompletedAt    time.Time
	Metadata       map[string]any
}

type Run struct {
	RunID           string
	SessionID       string
	TurnID          string
	AgentID         string
	Role            string
	ParentRunID     string
	ResumeFromRunID string
	State           RunState
	StartedAt       time.Time
	EndedAt         time.Time
	FailureCode     string
	Summary         string
	Metadata        map[string]any
}

type RunEvent struct {
	EventID   string
	SessionID string
	TurnID    string
	RunID     string
	AgentID   string
	EventType string
	Sequence  int64
	Payload   map[string]any
	Source    string
	CreatedAt time.Time
}

type ExternalTask struct {
	ExternalTaskID  string
	SessionID       string
	TurnID          string
	RunID           string
	Adapter         string
	RemoteAgentName string
	RemoteSessionID string
	RemoteTaskID    string
	State           string
	LastHeartbeatAt time.Time
	ResultSummary   string
	Metadata        map[string]any
}

type Recap struct {
	RecapID   string
	SessionID string
	TurnID    string
	RunID     string
	Kind      string
	Target    string
	Content   string
	CreatedAt time.Time
	Metadata  map[string]any
}

type MemoryCandidate struct {
	CandidateID   string
	SessionID     string
	TurnID        string
	RunID         string
	SourceRecapID string
	Target        string
	Kind          string
	Content       string
	Score         float64
	Status        MemoryCandidateStatus
	CreatedAt     time.Time
	Metadata      map[string]any
}
