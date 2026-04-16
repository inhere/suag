package session

import "time"

type SessionStatus string
type TurnStatus string
type RunState string
type InitiatorType string
type MemoryCandidateStatus string

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
