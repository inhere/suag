package agui

const (
	EventTypeStateSnapshot = "STATE_SNAPSHOT"
	EventTypeCustom        = "CUSTOM"
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
