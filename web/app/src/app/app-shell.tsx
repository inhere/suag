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
      </main>
      <section className="app-shell__panel" aria-label="Run Inspector panel">
        <h1>Run Inspector</h1>
      </section>
    </div>
  );
}
