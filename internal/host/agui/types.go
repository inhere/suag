package agui

import (
	"encoding/json"
	"fmt"
	"time"
)

type EventType string

const (
	EventTypeStateSnapshot      EventType = "STATE_SNAPSHOT"
	EventTypeCustom             EventType = "CUSTOM"
	EventTypeRunStarted         EventType = "RUN_STARTED"
	EventTypeRunFinished        EventType = "RUN_FINISHED"
	EventTypeRunError           EventType = "RUN_ERROR"
	EventTypeRunCanceled        EventType = "RUN_CANCELED"
	EventTypeActivitySnapshot   EventType = "ACTIVITY_SNAPSHOT"
	EventTypeTextMessageStart   EventType = "TEXT_MESSAGE_START"
	EventTypeTextMessageContent EventType = "TEXT_MESSAGE_CONTENT"
	EventTypeTextMessageEnd     EventType = "TEXT_MESSAGE_END"
)

const (
	StateSnapshotVersionV0 = "v0"
	StateSnapshotSchemaV0  = "suag.host.state.v0"
)

type Event interface {
	Type() EventType
	Timestamp() *int64
	SetTimestamp(timestamp int64)
	ThreadID() string
	RunID() string
	Validate() error
	ToJSON() ([]byte, error)
	GetBaseEvent() *BaseEvent
}

type BaseEvent struct {
	EventType   EventType `json:"type"`
	TimestampMs *int64    `json:"timestamp,omitempty"`
}

func NewBaseEvent(eventType EventType) *BaseEvent {
	now := time.Now().UnixMilli()
	return &BaseEvent{
		EventType:   eventType,
		TimestampMs: &now,
	}
}

func (b *BaseEvent) Type() EventType {
	return b.EventType
}

func (b *BaseEvent) Timestamp() *int64 {
	return b.TimestampMs
}

func (b *BaseEvent) SetTimestamp(timestamp int64) {
	b.TimestampMs = &timestamp
}

func (b *BaseEvent) ThreadID() string {
	return ""
}

func (b *BaseEvent) RunID() string {
	return ""
}

func (b *BaseEvent) Validate() error {
	if b.EventType == "" {
		return fmt.Errorf("event type is required")
	}
	return nil
}

func (b *BaseEvent) ToJSON() ([]byte, error) {
	return json.Marshal(b)
}

func (b *BaseEvent) GetBaseEvent() *BaseEvent {
	return b
}

type HostEvent struct {
	Type      string
	SessionID string
	TurnID    string
	RunID     string
	AgentID   string
	Sequence  int64
	Payload   any
	// RunMetadata mirrors host-persisted run metadata so AG-UI mapping can use
	// the stable host contract instead of re-deriving loop semantics from events.
	RunMetadata map[string]any
	CreatedAt time.Time
}

type StateSnapshotEvent struct {
	*BaseEvent
	Snapshot any `json:"snapshot"`
}

func NewStateSnapshotEvent(snapshot any) *StateSnapshotEvent {
	return &StateSnapshotEvent{
		BaseEvent: NewBaseEvent(EventTypeStateSnapshot),
		Snapshot:  snapshot,
	}
}

func (e *StateSnapshotEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.Snapshot == nil {
		return fmt.Errorf("snapshot is required")
	}
	return nil
}

func (e *StateSnapshotEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type CustomEvent struct {
	*BaseEvent
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}

func NewCustomEvent(name string, value any) *CustomEvent {
	return &CustomEvent{
		BaseEvent: NewBaseEvent(EventTypeCustom),
		Name:      name,
		Value:     value,
	}
}

func (e *CustomEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

func (e *CustomEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type RunStartedEvent struct {
	*BaseEvent
	ThreadIDValue string `json:"threadId"`
	RunIDValue    string `json:"runId"`
}

func NewRunStartedEvent(threadID, runID string) *RunStartedEvent {
	return &RunStartedEvent{
		BaseEvent:     NewBaseEvent(EventTypeRunStarted),
		ThreadIDValue: threadID,
		RunIDValue:    runID,
	}
}

func (e *RunStartedEvent) ThreadID() string {
	return e.ThreadIDValue
}

func (e *RunStartedEvent) RunID() string {
	return e.RunIDValue
}

func (e *RunStartedEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.ThreadIDValue == "" || e.RunIDValue == "" {
		return fmt.Errorf("threadId and runId are required")
	}
	return nil
}

func (e *RunStartedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type RunFinishedEvent struct {
	*BaseEvent
	ThreadIDValue string `json:"threadId"`
	RunIDValue    string `json:"runId"`
	Result        any    `json:"result,omitempty"`
}

func NewRunFinishedEvent(threadID, runID string, result any) *RunFinishedEvent {
	return &RunFinishedEvent{
		BaseEvent:     NewBaseEvent(EventTypeRunFinished),
		ThreadIDValue: threadID,
		RunIDValue:    runID,
		Result:        result,
	}
}

func (e *RunFinishedEvent) ThreadID() string {
	return e.ThreadIDValue
}

func (e *RunFinishedEvent) RunID() string {
	return e.RunIDValue
}

func (e *RunFinishedEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.ThreadIDValue == "" || e.RunIDValue == "" {
		return fmt.Errorf("threadId and runId are required")
	}
	return nil
}

func (e *RunFinishedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type RunErrorEvent struct {
	*BaseEvent
	RunIDValue string `json:"runId,omitempty"`
	Message    string `json:"message"`
}

func NewRunErrorEvent(runID, message string) *RunErrorEvent {
	return &RunErrorEvent{
		BaseEvent:  NewBaseEvent(EventTypeRunError),
		RunIDValue: runID,
		Message:    message,
	}
}

func (e *RunErrorEvent) RunID() string {
	return e.RunIDValue
}

func (e *RunErrorEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.Message == "" {
		return fmt.Errorf("message is required")
	}
	return nil
}

func (e *RunErrorEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type RunCanceledEvent struct {
	*BaseEvent
	RunIDValue string `json:"runId,omitempty"`
	Message    string `json:"message"`
}

func NewRunCanceledEvent(runID, message string) *RunCanceledEvent {
	return &RunCanceledEvent{
		BaseEvent:  NewBaseEvent(EventTypeRunCanceled),
		RunIDValue: runID,
		Message:    message,
	}
}

func (e *RunCanceledEvent) RunID() string {
	return e.RunIDValue
}

func (e *RunCanceledEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.Message == "" {
		return fmt.Errorf("message is required")
	}
	return nil
}

func (e *RunCanceledEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type ActivitySnapshotEvent struct {
	*BaseEvent
	MessageID    string `json:"messageId"`
	ActivityType string `json:"activityType"`
	Content      any    `json:"content"`
	Replace      *bool  `json:"replace,omitempty"`
}

func NewActivitySnapshotEvent(messageID, activityType string, content any) *ActivitySnapshotEvent {
	replace := true
	return &ActivitySnapshotEvent{
		BaseEvent:    NewBaseEvent(EventTypeActivitySnapshot),
		MessageID:    messageID,
		ActivityType: activityType,
		Content:      content,
		Replace:      &replace,
	}
}

func (e *ActivitySnapshotEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.MessageID == "" || e.ActivityType == "" || e.Content == nil {
		return fmt.Errorf("messageId, activityType and content are required")
	}
	return nil
}

func (e *ActivitySnapshotEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type TextMessageStartEvent struct {
	*BaseEvent
	MessageID string  `json:"messageId"`
	Role      *string `json:"role,omitempty"`
}

func NewTextMessageStartEvent(messageID string, role string) *TextMessageStartEvent {
	event := &TextMessageStartEvent{
		BaseEvent: NewBaseEvent(EventTypeTextMessageStart),
		MessageID: messageID,
	}
	if role != "" {
		event.Role = &role
	}
	return event
}

func (e *TextMessageStartEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.MessageID == "" {
		return fmt.Errorf("messageId is required")
	}
	return nil
}

func (e *TextMessageStartEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type TextMessageContentEvent struct {
	*BaseEvent
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
}

func NewTextMessageContentEvent(messageID, delta string) *TextMessageContentEvent {
	return &TextMessageContentEvent{
		BaseEvent: NewBaseEvent(EventTypeTextMessageContent),
		MessageID: messageID,
		Delta:     delta,
	}
}

func (e *TextMessageContentEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.MessageID == "" || e.Delta == "" {
		return fmt.Errorf("messageId and delta are required")
	}
	return nil
}

func (e *TextMessageContentEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

type TextMessageEndEvent struct {
	*BaseEvent
	MessageID string `json:"messageId"`
}

func NewTextMessageEndEvent(messageID string) *TextMessageEndEvent {
	return &TextMessageEndEvent{
		BaseEvent: NewBaseEvent(EventTypeTextMessageEnd),
		MessageID: messageID,
	}
}

func (e *TextMessageEndEvent) Validate() error {
	if err := e.BaseEvent.Validate(); err != nil {
		return err
	}
	if e.MessageID == "" {
		return fmt.Errorf("messageId is required")
	}
	return nil
}

func (e *TextMessageEndEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
