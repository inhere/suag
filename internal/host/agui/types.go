package agui

const (
	EventTypeStateSnapshot      = "STATE_SNAPSHOT"
	EventTypeCustom             = "CUSTOM"
	EventTypeRunStarted         = "RUN_STARTED"
	EventTypeRunFinished        = "RUN_FINISHED"
	EventTypeRunError           = "RUN_ERROR"
	EventTypeActivitySnapshot   = "ACTIVITY_SNAPSHOT"
	EventTypeTextMessageStart   = "TEXT_MESSAGE_START"
	EventTypeTextMessageContent = "TEXT_MESSAGE_CONTENT"
	EventTypeTextMessageEnd     = "TEXT_MESSAGE_END"
)

type Event struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type HostEvent struct {
	Type      string
	SessionID string
	TurnID    string
	RunID     string
	AgentID   string
	Sequence  int64
	Payload   any
}

type StateSnapshotEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Snapshot  any    `json:"snapshot"`
}

type CustomEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Name      string `json:"name"`
	Value     any    `json:"value,omitempty"`
}

type RunStartedEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	ThreadID  string `json:"threadId"`
	RunID     string `json:"runId"`
}

type RunFinishedEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	ThreadID  string `json:"threadId"`
	RunID     string `json:"runId"`
	Result    any    `json:"result,omitempty"`
}

type RunErrorEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	RunID     string `json:"runId,omitempty"`
	Message   string `json:"message"`
}

type ActivitySnapshotEvent struct {
	Type         string `json:"type"`
	Timestamp    int64  `json:"timestamp,omitempty"`
	MessageID    string `json:"messageId"`
	ActivityType string `json:"activityType"`
	Content      any    `json:"content"`
}

type TextMessageStartEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	MessageID string `json:"messageId"`
	Role      string `json:"role,omitempty"`
}

type TextMessageContentEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
}

type TextMessageEndEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	MessageID string `json:"messageId"`
}
