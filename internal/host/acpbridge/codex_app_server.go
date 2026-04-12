package acpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/inhere/axon/internal/host/session"
)

type CodexAppServerBridgeConfig struct {
	Commands       map[string]CommandConfig
	SessionStarter ACPStdioSessionStarter
	Now            func() time.Time
}

type CodexAppServerBridge struct {
	commands map[string]CommandConfig
	starter  ACPStdioSessionStarter
	now      func() time.Time
}

type codexInitializeResult struct {
	ServerInfo map[string]any `json:"serverInfo,omitempty"`
}

type codexThreadStartResult struct {
	ThreadID string          `json:"threadId,omitempty"`
	Thread   *codexThreadRef `json:"thread,omitempty"`
}

type codexTurnStartResult struct {
	TurnID string        `json:"turnId,omitempty"`
	Turn   *codexTurnRef `json:"turn,omitempty"`
}

type codexThreadRef struct {
	ID string `json:"id,omitempty"`
}

type codexTurnRef struct {
	ID     string          `json:"id,omitempty"`
	Status string          `json:"status,omitempty"`
	Error  *codexTurnError `json:"error,omitempty"`
}

type codexTurnError struct {
	Message           string `json:"message,omitempty"`
	AdditionalDetails string `json:"additionalDetails,omitempty"`
}

type codexTurnStartedNotification struct {
	ThreadID string        `json:"threadId"`
	TurnID   string        `json:"turnId,omitempty"`
	Turn     *codexTurnRef `json:"turn,omitempty"`
}

type codexAgentMessageDeltaNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId,omitempty"`
	Delta    string `json:"delta,omitempty"`
}

type codexTurnCompletedNotification struct {
	ThreadID   string        `json:"threadId"`
	TurnID     string        `json:"turnId,omitempty"`
	StopReason string        `json:"stopReason,omitempty"`
	Turn       *codexTurnRef `json:"turn,omitempty"`
}

type codexErrorNotification struct {
	ThreadID  string         `json:"threadId"`
	TurnID    string         `json:"turnId"`
	Error     codexTurnError `json:"error"`
	WillRetry bool           `json:"willRetry"`
}

type codexItemStartedNotification struct {
	ThreadID string             `json:"threadId"`
	TurnID   string             `json:"turnId"`
	ItemID   string             `json:"itemId,omitempty"`
	ItemType string             `json:"itemType,omitempty"`
	Item     *codexItemSnapshot `json:"item,omitempty"`
}

type codexItemCompletedNotification struct {
	ThreadID string             `json:"threadId"`
	TurnID   string             `json:"turnId"`
	ItemID   string             `json:"itemId,omitempty"`
	ItemType string             `json:"itemType,omitempty"`
	Item     *codexItemSnapshot `json:"item,omitempty"`
}

type codexCommandExecutionDeltaNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta,omitempty"`
}

type codexItemSnapshot struct {
	ID               string `json:"id,omitempty"`
	Type             string `json:"type,omitempty"`
	Text             string `json:"text,omitempty"`
	Command          string `json:"command,omitempty"`
	CWD              string `json:"cwd,omitempty"`
	Status           string `json:"status,omitempty"`
	AggregatedOutput string `json:"aggregatedOutput,omitempty"`
	ExitCode         *int   `json:"exitCode,omitempty"`
}

type codexItemTrace struct {
	ItemID        string
	ItemType      string
	Command       string
	CWD           string
	Status        string
	OutputPreview string
	Started       bool
	Completed     bool
	CommandDeltas int
}

type codexTurnTrace struct {
	StopReason        string
	LastError         string
	MessageDeltaCount int
	CommandDeltaCount int
	Items             map[string]*codexItemTrace
	MessageItemIDs    map[string]struct{}
}

func NewCodexAppServerBridge(cfg CodexAppServerBridgeConfig) *CodexAppServerBridge {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	starter := cfg.SessionStarter
	if starter == nil {
		starter = startACPStdioSession
	}

	return &CodexAppServerBridge{
		commands: copyCommandConfigs(cfg.Commands),
		starter:  starter,
		now:      nowFn,
	}
}

func (b *CodexAppServerBridge) Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error) {
	if b == nil {
		return DispatchResult{}, fmt.Errorf("codex app-server bridge is required")
	}
	if strings.TrimSpace(req.Adapter) == "" {
		return DispatchResult{}, fmt.Errorf("adapter is required")
	}
	if strings.TrimSpace(req.Instruction) == "" {
		return DispatchResult{}, fmt.Errorf("instruction is required")
	}

	cmdCfg, ok := b.commands[strings.TrimSpace(req.Adapter)]
	if !ok {
		return DispatchResult{}, fmt.Errorf("adapter %q is not configured", req.Adapter)
	}
	if len(cmdCfg.Command) == 0 {
		return DispatchResult{}, fmt.Errorf("adapter %q command is empty", req.Adapter)
	}

	spec := CommandSpec{
		Args:    expandCommandArgs(cmdCfg.Command, req),
		Workdir: strings.TrimSpace(req.Workspace),
		Env:     buildCommandEnv(cmdCfg.Env, req),
	}

	sess, err := b.starter(ctx, spec)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("start codex app-server session: %w", err)
	}
	defer sess.Close()

	if err := b.sendInitialize(ctx, sess); err != nil {
		return DispatchResult{}, err
	}
	if err := b.sendInitialized(ctx, sess); err != nil {
		return DispatchResult{}, err
	}

	threadID, resumed, err := b.sendThreadStart(ctx, sess, req)
	if err != nil {
		return DispatchResult{}, err
	}

	turnID, err := b.sendTurnStart(ctx, sess, threadID, req.Instruction)
	if err != nil {
		return DispatchResult{}, err
	}

	result, err := b.collectTurnResult(ctx, sess, req, threadID, turnID)
	if err != nil {
		return DispatchResult{}, err
	}
	if resumed {
		result.Metadata = mergeMetadata(result.Metadata, map[string]any{
			"resumedFromRemoteSessionId": threadID,
		})
	}
	return result, nil
}

func (b *CodexAppServerBridge) sendInitialize(ctx context.Context, sess ACPStdioSession) error {
	req := acpRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"clientInfo": map[string]string{
				"name":    "axon",
				"version": "v0",
			},
			"capabilities": map[string]any{},
		},
	}

	var result codexInitializeResult
	if err := sendRPCRequestExpectResult(ctx, sess, req, &result, nil); err != nil {
		return err
	}
	return nil
}

func (b *CodexAppServerBridge) sendInitialized(ctx context.Context, sess ACPStdioSession) error {
	req := acpRPCRequest{
		JSONRPC: "2.0",
		Method:  "initialized",
		Params:  map[string]any{},
	}
	if err := writeACPLine(ctx, sess, req); err != nil {
		return fmt.Errorf("write initialized notification: %w", err)
	}
	return nil
}

func (b *CodexAppServerBridge) sendThreadStart(ctx context.Context, sess ACPStdioSession, req DispatchRequest) (string, bool, error) {
	resumeThreadID := strings.TrimSpace(stringMapValue(req.Context, "resume_remote_session_id"))

	rpcReq := acpRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "thread/start",
		Params: map[string]any{
			"cwd": req.Workspace,
		},
	}
	resumed := false
	if resumeThreadID != "" {
		rpcReq.Method = "thread/resume"
		rpcReq.Params = map[string]any{
			"threadId": resumeThreadID,
			"cwd":      req.Workspace,
		}
		resumed = true
	}

	var result codexThreadStartResult
	if err := sendRPCRequestExpectResult(ctx, sess, rpcReq, &result, nil); err != nil {
		return "", false, err
	}

	threadID := strings.TrimSpace(result.ThreadID)
	if threadID == "" && result.Thread != nil {
		threadID = strings.TrimSpace(result.Thread.ID)
	}
	if threadID == "" {
		return "", false, fmt.Errorf("%s returned empty thread id", rpcReq.Method)
	}
	return threadID, resumed, nil
}

func (b *CodexAppServerBridge) sendTurnStart(ctx context.Context, sess ACPStdioSession, threadID, instruction string) (string, error) {
	req := acpRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "turn/start",
		Params: map[string]any{
			"threadId": threadID,
			"input": []map[string]any{
				{
					"type": "text",
					"text": instruction,
				},
			},
		},
	}

	var result codexTurnStartResult
	if err := sendRPCRequestExpectResult(ctx, sess, req, &result, nil); err != nil {
		return "", err
	}

	turnID := strings.TrimSpace(result.TurnID)
	if turnID == "" && result.Turn != nil {
		turnID = strings.TrimSpace(result.Turn.ID)
	}
	if turnID == "" {
		return "", fmt.Errorf("turn/start returned empty turn id")
	}
	return turnID, nil
}

func (b *CodexAppServerBridge) collectTurnResult(
	ctx context.Context,
	sess ACPStdioSession,
	req DispatchRequest,
	threadID string,
	turnID string,
) (DispatchResult, error) {
	var summaryBuilder strings.Builder
	latestError := ""
	result := DispatchResult{
		State:           StateCompleted,
		RemoteAgentName: req.Adapter,
		RemoteSessionID: threadID,
		RemoteTaskID:    turnID,
		LastHeartbeatAt: b.now().UTC(),
		Metadata: map[string]any{
			"protocol": "codex_app_server",
		},
	}
	trace := codexTurnTrace{
		Items:          map[string]*codexItemTrace{},
		MessageItemIDs: map[string]struct{}{},
	}

	for {
		msg, err := readACPResponse(ctx, sess)
		if err != nil {
			return DispatchResult{}, fmt.Errorf("read codex turn stream: %w", err)
		}
		if msg.Method == "" {
			continue
		}

		switch msg.Method {
		case "turn/started":
			var note codexTurnStartedNotification
			if err := json.Unmarshal(msg.Params, &note); err != nil {
				return DispatchResult{}, fmt.Errorf("decode turn/started notification: %w", err)
			}
			if effectiveCodexTurnID(note.TurnID, note.Turn) != "" {
				result.RemoteTaskID = effectiveCodexTurnID(note.TurnID, note.Turn)
			}
			emitDispatchEvent(req, DispatchEvent{
				Type: "external.activity",
				Payload: map[string]any{
					"kind":            "turn_started",
					"remoteSessionId": result.RemoteSessionID,
					"remoteTaskId":    result.RemoteTaskID,
				},
			})
		case "item/agentMessage/delta":
			var note codexAgentMessageDeltaNotification
			if err := json.Unmarshal(msg.Params, &note); err != nil {
				return DispatchResult{}, fmt.Errorf("decode item/agentMessage/delta notification: %w", err)
			}
			if strings.TrimSpace(note.TurnID) != "" && result.RemoteTaskID == "" {
				result.RemoteTaskID = strings.TrimSpace(note.TurnID)
			}
			trace.MessageDeltaCount++
			messageID := buildExternalMessageID(result.RemoteTaskID, note.ItemID)
			_, seen := trace.MessageItemIDs[messageID]
			trace.MessageItemIDs[messageID] = struct{}{}
			emitDispatchEvent(req, DispatchEvent{
				Type: "external.message.delta",
				Payload: map[string]any{
					"messageId":       messageID,
					"itemId":          strings.TrimSpace(note.ItemID),
					"delta":           note.Delta,
					"isFirstChunk":    !seen,
					"remoteSessionId": result.RemoteSessionID,
					"remoteTaskId":    firstNonEmpty(strings.TrimSpace(note.TurnID), result.RemoteTaskID),
				},
			})
			summaryBuilder.WriteString(note.Delta)
		case "item/started":
			var note codexItemStartedNotification
			if err := json.Unmarshal(msg.Params, &note); err != nil {
				return DispatchResult{}, fmt.Errorf("decode item/started notification: %w", err)
			}
			itemTrace := ensureCodexItemTrace(trace.Items, firstNonEmpty(strings.TrimSpace(note.ItemID), itemIDFromSnapshot(note.Item)))
			itemTrace.Started = true
			itemTrace.ItemType = firstNonEmpty(strings.TrimSpace(note.ItemType), itemTypeFromSnapshot(note.Item), itemTrace.ItemType)
			mergeCodexItemSnapshot(itemTrace, note.Item)
			emitDispatchEvent(req, DispatchEvent{
				Type: "external.activity",
				Payload: map[string]any{
					"kind":            "item_started",
					"itemId":          itemTrace.ItemID,
					"itemType":        itemTrace.ItemType,
					"command":         itemTrace.Command,
					"cwd":             itemTrace.CWD,
					"status":          itemTrace.Status,
					"remoteSessionId": result.RemoteSessionID,
					"remoteTaskId":    firstNonEmpty(strings.TrimSpace(note.TurnID), result.RemoteTaskID),
				},
			})
		case "item/completed":
			var note codexItemCompletedNotification
			if err := json.Unmarshal(msg.Params, &note); err != nil {
				return DispatchResult{}, fmt.Errorf("decode item/completed notification: %w", err)
			}
			itemTrace := ensureCodexItemTrace(trace.Items, firstNonEmpty(strings.TrimSpace(note.ItemID), itemIDFromSnapshot(note.Item)))
			itemTrace.Completed = true
			itemTrace.ItemType = firstNonEmpty(strings.TrimSpace(note.ItemType), itemTypeFromSnapshot(note.Item), itemTrace.ItemType)
			mergeCodexItemSnapshot(itemTrace, note.Item)
			emitDispatchEvent(req, DispatchEvent{
				Type: "external.activity",
				Payload: map[string]any{
					"kind":            "item_completed",
					"itemId":          itemTrace.ItemID,
					"itemType":        itemTrace.ItemType,
					"command":         itemTrace.Command,
					"cwd":             itemTrace.CWD,
					"status":          itemTrace.Status,
					"outputPreview":   itemTrace.OutputPreview,
					"remoteSessionId": result.RemoteSessionID,
					"remoteTaskId":    firstNonEmpty(strings.TrimSpace(note.TurnID), result.RemoteTaskID),
				},
			})
		case "item/commandExecution/outputDelta":
			var note codexCommandExecutionDeltaNotification
			if err := json.Unmarshal(msg.Params, &note); err != nil {
				return DispatchResult{}, fmt.Errorf("decode item/commandExecution/outputDelta notification: %w", err)
			}
			itemTrace := ensureCodexItemTrace(trace.Items, strings.TrimSpace(note.ItemID))
			itemTrace.ItemType = firstNonEmpty(itemTrace.ItemType, "commandExecution")
			itemTrace.CommandDeltas++
			itemTrace.OutputPreview = trimPreview(firstNonEmpty(itemTrace.OutputPreview, note.Delta))
			trace.CommandDeltaCount++
			emitDispatchEvent(req, DispatchEvent{
				Type: "external.activity",
				Payload: map[string]any{
					"kind":            "command_output_delta",
					"itemId":          itemTrace.ItemID,
					"itemType":        itemTrace.ItemType,
					"delta":           trimPreview(note.Delta),
					"remoteSessionId": result.RemoteSessionID,
					"remoteTaskId":    firstNonEmpty(strings.TrimSpace(note.TurnID), result.RemoteTaskID),
				},
			})
		case "error":
			var note codexErrorNotification
			if err := json.Unmarshal(msg.Params, &note); err != nil {
				return DispatchResult{}, fmt.Errorf("decode error notification: %w", err)
			}
			if strings.TrimSpace(note.TurnID) != "" {
				result.RemoteTaskID = strings.TrimSpace(note.TurnID)
			}
			latestError = formatCodexTurnError(note.Error)
			trace.LastError = latestError
			emitDispatchEvent(req, DispatchEvent{
				Type: "external.activity",
				Payload: map[string]any{
					"kind":            "turn_error",
					"message":         latestError,
					"remoteSessionId": result.RemoteSessionID,
					"remoteTaskId":    firstNonEmpty(strings.TrimSpace(note.TurnID), result.RemoteTaskID),
				},
			})
		case "turn/completed":
			var note codexTurnCompletedNotification
			if err := json.Unmarshal(msg.Params, &note); err != nil {
				return DispatchResult{}, fmt.Errorf("decode turn/completed notification: %w", err)
			}
			if strings.TrimSpace(note.ThreadID) != "" {
				result.RemoteSessionID = strings.TrimSpace(note.ThreadID)
			}
			if effectiveCodexTurnID(note.TurnID, note.Turn) != "" {
				result.RemoteTaskID = effectiveCodexTurnID(note.TurnID, note.Turn)
			}
			if note.Turn != nil && strings.EqualFold(strings.TrimSpace(note.Turn.Status), "failed") {
				result.State = StateFailed
				result.Error = firstNonEmpty(
					formatCodexTurnErrorPtr(note.Turn.Error),
					latestError,
					"codex turn failed",
				)
			} else if latestError != "" && strings.EqualFold(strings.TrimSpace(note.StopReason), "error") {
				result.State = StateFailed
				result.Error = latestError
			}
			trace.StopReason = strings.TrimSpace(note.StopReason)
			emitDispatchEvent(req, DispatchEvent{
				Type: "external.activity",
				Payload: map[string]any{
					"kind":            "turn_completed",
					"stopReason":      trace.StopReason,
					"remoteSessionId": result.RemoteSessionID,
					"remoteTaskId":    result.RemoteTaskID,
				},
			})
			for messageID := range trace.MessageItemIDs {
				emitDispatchEvent(req, DispatchEvent{
					Type: "external.message.end",
					Payload: map[string]any{
						"messageId":       messageID,
						"remoteSessionId": result.RemoteSessionID,
						"remoteTaskId":    result.RemoteTaskID,
					},
				})
			}
			result.Summary = strings.TrimSpace(summaryBuilder.String())
			result.Metadata = mergeMetadata(result.Metadata, buildCodexTurnMetadata(trace))
			if result.RemoteTaskID == "" {
				result.RemoteTaskID = session.NewExternalTaskID()
			}
			return result, nil
		}
	}
}

func ensureCodexItemTrace(items map[string]*codexItemTrace, itemID string) *codexItemTrace {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		itemID = "unknown"
	}
	if trace, ok := items[itemID]; ok {
		return trace
	}
	trace := &codexItemTrace{
		ItemID: itemID,
	}
	items[itemID] = trace
	return trace
}

func mergeCodexItemSnapshot(dst *codexItemTrace, snapshot *codexItemSnapshot) {
	if dst == nil || snapshot == nil {
		return
	}
	dst.ItemID = firstNonEmpty(dst.ItemID, strings.TrimSpace(snapshot.ID))
	dst.ItemType = firstNonEmpty(strings.TrimSpace(snapshot.Type), dst.ItemType)
	dst.Command = firstNonEmpty(strings.TrimSpace(snapshot.Command), dst.Command)
	dst.CWD = firstNonEmpty(strings.TrimSpace(snapshot.CWD), dst.CWD)
	dst.Status = firstNonEmpty(strings.TrimSpace(snapshot.Status), dst.Status)
	dst.OutputPreview = trimPreview(firstNonEmpty(strings.TrimSpace(snapshot.AggregatedOutput), dst.OutputPreview))
}

func itemIDFromSnapshot(snapshot *codexItemSnapshot) string {
	if snapshot == nil {
		return ""
	}
	return strings.TrimSpace(snapshot.ID)
}

func itemTypeFromSnapshot(snapshot *codexItemSnapshot) string {
	if snapshot == nil {
		return ""
	}
	return strings.TrimSpace(snapshot.Type)
}

func buildCodexTurnMetadata(trace codexTurnTrace) map[string]any {
	items := make([]map[string]any, 0, len(trace.Items))
	for _, item := range trace.Items {
		items = append(items, map[string]any{
			"itemId":        item.ItemID,
			"type":          item.ItemType,
			"command":       item.Command,
			"cwd":           item.CWD,
			"status":        item.Status,
			"started":       item.Started,
			"completed":     item.Completed,
			"commandDeltas": item.CommandDeltas,
			"outputPreview": item.OutputPreview,
		})
	}

	metadata := map[string]any{
		"eventSummary": map[string]any{
			"messageDeltaCount": trace.MessageDeltaCount,
			"commandDeltaCount": trace.CommandDeltaCount,
			"itemCount":         len(items),
		},
		"items": items,
	}
	if trace.StopReason != "" {
		metadata["stopReason"] = trace.StopReason
	}
	if trace.LastError != "" {
		metadata["lastError"] = trace.LastError
	}
	return metadata
}

func trimPreview(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 240 {
		return text
	}
	return strings.TrimSpace(text[:240])
}

func stringMapValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := raw.(string)
	return text
}

func emitDispatchEvent(req DispatchRequest, event DispatchEvent) {
	if req.Emit == nil {
		return
	}
	_ = req.Emit(event)
}

func buildExternalMessageID(remoteTaskID, itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		itemID = "message"
	}
	return firstNonEmpty(strings.TrimSpace(remoteTaskID), "external") + ":" + itemID
}

func effectiveCodexTurnID(turnID string, turn *codexTurnRef) string {
	id := strings.TrimSpace(turnID)
	if id != "" {
		return id
	}
	if turn == nil {
		return ""
	}
	return strings.TrimSpace(turn.ID)
}

func formatCodexTurnErrorPtr(turnErr *codexTurnError) string {
	if turnErr == nil {
		return ""
	}
	return formatCodexTurnError(*turnErr)
}

func formatCodexTurnError(turnErr codexTurnError) string {
	message := strings.TrimSpace(turnErr.Message)
	additional := strings.TrimSpace(turnErr.AdditionalDetails)
	if additional == "" || strings.EqualFold(additional, message) {
		return message
	}
	if message == "" {
		return additional
	}
	return message + ": " + additional
}

func sendRPCRequestExpectResult(
	ctx context.Context,
	sess ACPStdioSession,
	req acpRPCRequest,
	out any,
	ignoreNotification func(acpRPCResponse) bool,
) error {
	if err := writeACPLine(ctx, sess, req); err != nil {
		return fmt.Errorf("write %s request: %w", req.Method, err)
	}

	for {
		msg, err := readACPResponse(ctx, sess)
		if err != nil {
			return fmt.Errorf("read %s response: %w", req.Method, err)
		}
		if msg.Method != "" {
			if ignoreNotification != nil && ignoreNotification(msg) {
				continue
			}
			continue
		}
		if msg.ID != req.ID {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("%s error: %s", req.Method, strings.TrimSpace(msg.Error.Message))
		}
		if out != nil && len(msg.Result) > 0 {
			if err := json.Unmarshal(msg.Result, out); err != nil {
				return fmt.Errorf("decode %s result: %w", req.Method, err)
			}
		}
		return nil
	}
}
