import { ConversationPane } from "../features/conversation/conversation-pane";
import { RunInspector } from "../features/run-inspector/run-inspector";
import { SessionList } from "../features/session-list/session-list";

export function AppShell() {
  const sessions = [
    {
      sessionId: "sess_001",
      title: "host runtime",
      status: "RUNNING",
      updatedAt: "2m",
      preview: "正在整理页面结构",
    },
  ] as const;

  const messages = [
    { id: "user-1", role: "user" as const, text: "请整理当前页面结构" },
    { id: "assistant-1", role: "assistant" as const, text: "已开始处理这个任务" },
  ];

  const currentTurn = {
    turnId: "turn_012",
    sessionId: "sess_001",
    primaryRunId: "run_010",
    currentRunId: "run_011",
    latestRunId: "run_011",
    latestAgentId: "default",
    displayState: "external_running",
    latestEventType: "run.started",
    latestMessagePreview: "正在生成页面草图",
    eventCount: 4,
    runCount: 2,
    activeRunCount: 2,
    completedRunCount: 0,
    failedRunCount: 0,
    activeRunIds: ["run_010", "run_011"],
    completedRunIds: [],
    failedRunIds: [],
    hasExternalWork: true,
    updatedAt: "2026-04-12T10:00:00Z",
  } as const;

  const runs = [
    {
      runId: "run_011",
      turnId: "turn_012",
      sessionId: "sess_001",
      agentId: "default",
      displayState: "external_running",
      latestEventType: "run.started",
      latestMessageId: "msg_011",
      latestMessageText: "正在生成页面草图",
      summary: "",
      externalTaskIds: ["ext_001"],
      eventCount: 4,
      updatedAt: "2026-04-12T10:00:00Z",
    },
  ] as const;

  const externalTasks = [
    {
      externalTaskId: "ext_001",
      sessionId: "sess_001",
      turnId: "turn_012",
      runId: "run_011",
      adapter: "codex",
      remoteAgentName: "codex-cli",
      remoteSessionId: "remote_sess_001",
      remoteTaskId: "remote_task_001",
      state: "running",
      lastHeartbeatAt: "2026-04-12T10:00:00Z",
      resultSummary: "",
      metadata: { protocol: "codex_app_server" },
    },
  ] as const;

  return (
    <div className="app-shell">
      <aside className="app-shell__panel" aria-label="Sessions panel">
        <SessionList
          sessions={[...sessions]}
          currentSessionId="sess_001"
          filter="all"
          searchQuery=""
          onSelect={() => {}}
          onSearchChange={() => {}}
          onFilterChange={() => {}}
        />
      </aside>
      <main className="app-shell__panel" aria-label="Conversation panel">
        <h1>Conversation</h1>
        <ConversationPane
          messages={messages}
          currentTurn={currentTurn}
          waitingForInput={false}
          onSubmit={() => {}}
        />
      </main>
      <section className="app-shell__panel" aria-label="Run Inspector panel">
        <h1>Run Inspector</h1>
        <RunInspector
          currentTurn={currentTurn}
          runs={[...runs]}
          externalTasks={[...externalTasks]}
          focusedRunId="run_011"
          onFocusRun={() => {}}
        />
      </section>
    </div>
  );
}
