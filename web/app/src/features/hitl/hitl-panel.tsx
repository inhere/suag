import { useEffect, useState } from "react";
import type { HITLTicket } from "../../lib/host-types";

type HITLPanelProps = {
  tickets: HITLTicket[];
  loading: boolean;
  currentSessionId?: string | null;
  onRefresh: () => void;
  onOpenContext?: (ticket: HITLTicket) => void;
  onRespond: (ticketId: string, instruction: string) => Promise<void> | void;
  onDismiss: (ticketId: string) => Promise<void> | void;
};

export function HITLPanel({
  tickets,
  loading,
  currentSessionId = null,
  onRefresh,
  onOpenContext = () => {},
  onRespond,
  onDismiss,
}: HITLPanelProps) {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [busyTicketId, setBusyTicketId] = useState<string | null>(null);

  useEffect(() => {
    setDrafts((current) => {
      const next: Record<string, string> = {};
      for (const ticket of tickets) {
        next[ticket.id] = current[ticket.id] ?? "";
      }
      return next;
    });
  }, [tickets]);

  async function handleRespond(ticketId: string) {
    const instruction = drafts[ticketId]?.trim() ?? "";
    if (!instruction) {
      return;
    }

    setBusyTicketId(ticketId);
    try {
      await onRespond(ticketId, instruction);
      setDrafts((current) => ({
        ...current,
        [ticketId]: "",
      }));
    } finally {
      setBusyTicketId(null);
    }
  }

  async function handleDismiss(ticketId: string) {
    setBusyTicketId(ticketId);
    try {
      await onDismiss(ticketId);
    } finally {
      setBusyTicketId(null);
    }
  }

  return (
    <section className="hitl-panel" aria-label="HITL panel">
      <div className="hitl-panel__header">
        <div>
          <h2>Pending HITL</h2>
          <p>{tickets.length ? `${tickets.length} ticket(s) waiting` : "No pending tickets."}</p>
        </div>
        <button type="button" onClick={onRefresh} disabled={loading}>
          {loading ? "Refreshing..." : "Refresh"}
        </button>
      </div>
      {tickets.length ? (
        <div className="hitl-panel__list">
          {tickets.map((ticket) => {
            const busy = busyTicketId === ticket.id;
            const linkedToCurrentSession =
              !!currentSessionId && !!ticket.sessionId && currentSessionId === ticket.sessionId;
            return (
              <article
                key={ticket.id}
                className={`hitl-card${linkedToCurrentSession ? " hitl-card--linked" : ""}`}
              >
                <div className="hitl-card__meta">
                  <strong>{ticket.reason}</strong>
                  <span>{ticket.agentId}</span>
                  <span>{ticket.taskId}</span>
                </div>
                {(ticket.sessionId || ticket.turnId || ticket.runId) ? (
                  <div className="hitl-card__links">
                    {ticket.sessionId ? <span>Session {ticket.sessionId}</span> : null}
                    {ticket.turnId ? <span>Turn {ticket.turnId}</span> : null}
                    {ticket.runId ? <span>Run {ticket.runId}</span> : null}
                  </div>
                ) : null}
                <p className="hitl-card__context">{ticket.context || "No extra context provided."}</p>
                <textarea
                  value={drafts[ticket.id] ?? ""}
                  onChange={(event) =>
                    setDrafts((current) => ({
                      ...current,
                      [ticket.id]: event.target.value,
                    }))
                  }
                  placeholder="给这个等待中的 ticket 一条人工指令..."
                  disabled={busy}
                />
                <div className="run-actions">
                  {(ticket.sessionId || ticket.runId) ? (
                    <button
                      type="button"
                      onClick={() => onOpenContext(ticket)}
                      disabled={busy}
                    >
                      Open Context
                    </button>
                  ) : null}
                  <button
                    type="button"
                    onClick={() => {
                      void handleRespond(ticket.id);
                    }}
                    disabled={busy || !(drafts[ticket.id]?.trim() ?? "")}
                  >
                    Respond
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      void handleDismiss(ticket.id);
                    }}
                    disabled={busy}
                  >
                    Dismiss
                  </button>
                </div>
              </article>
            );
          })}
        </div>
      ) : null}
    </section>
  );
}
