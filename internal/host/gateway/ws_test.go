package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/inhere/axon/internal/host/session"
	_ "modernc.org/sqlite"
)

func TestServer_CreateSessionAndStreamEventsOverWebSocket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	svc := session.NewService(store, bus, &fakeGatewayRunner{}, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "demo session",
	})
	if createResp.SessionID == "" {
		t.Fatal("create session response missing session_id")
	}

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}
	if connected["type"] != "connected" {
		t.Fatalf("connected event type = %v, want connected", connected["type"])
	}

	turnResp := postJSON[startTurnResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/turns", map[string]any{
		"agent_id":       "default",
		"message":        "implement feature x",
		"initiator_type": "user",
	})
	if turnResp.RunID == "" {
		t.Fatal("start turn response missing run_id")
	}

	var event Event
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("wsjson.Read() event error = %v", err)
	}
	if event.Type != "run.queued" {
		t.Fatalf("event type = %q, want %q", event.Type, "run.queued")
	}
	if event.SessionID != createResp.SessionID {
		t.Fatalf("event session_id = %q, want %q", event.SessionID, createResp.SessionID)
	}
	if event.RunID != turnResp.RunID {
		t.Fatalf("event run_id = %q, want %q", event.RunID, turnResp.RunID)
	}

	gotSession := getJSON[getSessionResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID)
	if gotSession.SessionID != createResp.SessionID {
		t.Fatalf("get session session_id = %q, want %q", gotSession.SessionID, createResp.SessionID)
	}
	if gotSession.LastTurnID != turnResp.TurnID {
		t.Fatalf("get session last_turn_id = %q, want %q", gotSession.LastTurnID, turnResp.TurnID)
	}

	events := getJSON[[]Event](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/events")
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Type != "run.queued" {
		t.Fatalf("events[0].type = %q, want %q", events[0].Type, "run.queued")
	}
}

type fakeGatewayRunner struct{}

func (fakeGatewayRunner) Run(context.Context, session.RuntimeRequest) (session.RuntimeResult, error) {
	return session.RuntimeResult{Summary: "ok"}, nil
}

func newGatewaySessionStore(t *testing.T) *session.SQLiteStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "gateway-session.db")
	applyGatewaySessionMigration(t, dbPath)

	store, err := session.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("session.NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}
	})
	return store
}

func applyGatewaySessionMigration(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close() error = %v", err)
		}
	}()

	upSQL := readGatewaySessionMigrationUp(t)
	if _, err := db.Exec(upSQL); err != nil {
		t.Fatalf("db.Exec() migration error = %v", err)
	}
}

func readGatewaySessionMigrationUp(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	migrationPath := filepath.Join(
		filepath.Dir(file),
		"..", "..", "..",
		"db", "migrations", "20260411-100000-create-host-session-foundation.sql",
	)
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	parts := strings.Split(string(content), "-- Migrate:DOWN")
	if len(parts) != 2 {
		t.Fatalf("unexpected migration format in %s", migrationPath)
	}

	upSQL := strings.TrimSpace(strings.TrimPrefix(parts[0], "-- Migrate:UP"))
	if upSQL == "" {
		t.Fatalf("empty migration up section in %s", migrationPath)
	}
	return upSQL
}

func postJSON[T any](t *testing.T, endpoint string, payload any) T {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("POST %s status = %d", endpoint, resp.StatusCode)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("json.NewDecoder().Decode() error = %v", err)
	}
	return out
}

func getJSON[T any](t *testing.T, endpoint string) T {
	t.Helper()

	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("GET %s status = %d", endpoint, resp.StatusCode)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("json.NewDecoder().Decode() error = %v", err)
	}
	return out
}

func mustHTTPToWS(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	return parsed.String()
}
