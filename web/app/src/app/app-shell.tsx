export function AppShell() {
  return (
    <div className="app-shell">
      <aside className="app-shell__panel" aria-label="Sessions panel">
        <h1>Sessions</h1>
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
