import type { MemoryCandidate } from "../../lib/host-types";

type MemoryCandidatesPanelProps = {
  candidates: MemoryCandidate[];
  loading?: boolean;
  errorMessage?: string | null;
  actionError?: string | null;
  pendingCandidateId?: string | null;
  onRetry?: () => void;
  onAccept?: (candidateId: string) => void;
  onReject?: (candidateId: string) => void;
};

const PRIMARY_GROUPS = ["MEMORY", "USER"] as const;

function groupCandidates(candidates: MemoryCandidate[]) {
  const grouped = new Map<string, MemoryCandidate[]>();

  for (const group of PRIMARY_GROUPS) {
    grouped.set(group, []);
  }

  for (const candidate of candidates) {
    const target = candidate.target;
    if (!grouped.has(target)) {
      grouped.set(target, []);
    }

    grouped.get(target)?.push(candidate);
  }

  const orderedGroups: Array<{ target: string; items: MemoryCandidate[] }> = [];
  for (const group of PRIMARY_GROUPS) {
    const items = grouped.get(group) ?? [];
    if (items.length) {
      orderedGroups.push({ target: group, items });
    }
    grouped.delete(group);
  }

  for (const [target, items] of grouped.entries()) {
    if (items.length) {
      orderedGroups.push({ target, items });
    }
  }

  return orderedGroups;
}

export function MemoryCandidatesPanel({
  candidates,
  loading = false,
  errorMessage = null,
  actionError = null,
  pendingCandidateId = null,
  onRetry = () => {},
  onAccept = () => {},
  onReject = () => {},
}: MemoryCandidatesPanelProps) {
  const groupedCandidates = groupCandidates(candidates);

  return (
    <section className="memory-candidates-panel run-panel" aria-label="Memory candidates panel">
      <div className="memory-candidates-panel__header">
        <h2>Memory Candidates</h2>
        <p>{candidates.length} candidate(s) pending</p>
      </div>

      {actionError ? <p className="inline-feedback inline-feedback--error">{actionError}</p> : null}

      {loading ? <p className="inline-feedback">Loading memory candidates...</p> : null}

      {!loading && errorMessage ? (
        <div className="memory-candidates-panel__group">
          <p className="inline-feedback inline-feedback--error">Failed to load memory candidates.</p>
          <div className="memory-candidate-card__actions run-actions">
            <button type="button" onClick={onRetry}>
              Retry
            </button>
          </div>
        </div>
      ) : null}

      {!loading && !errorMessage && !groupedCandidates.length ? (
        <p className="inline-feedback">No pending memory candidates.</p>
      ) : null}

      {!loading && !errorMessage
        ? groupedCandidates.map((group) => (
            <section key={group.target} className="memory-candidates-panel__group">
              <h3>{group.target}</h3>
              {group.items.map((candidate) => {
                const actionPending = pendingCandidateId === candidate.candidateId;
                return (
                  <article key={candidate.candidateId} className="memory-candidate-card hitl-card">
                    <div className="memory-candidate-card__meta">
                      <span>{candidate.kind}</span>
                      <span>{candidate.status}</span>
                    </div>
                    <p>{candidate.content}</p>
                    <div className="memory-candidate-card__actions run-actions">
                      <button
                        type="button"
                        disabled={actionPending}
                        onClick={() => onAccept(candidate.candidateId)}
                      >
                        Accept
                      </button>
                      <button
                        type="button"
                        disabled={actionPending}
                        onClick={() => onReject(candidate.candidateId)}
                      >
                        Reject
                      </button>
                    </div>
                  </article>
                );
              })}
            </section>
          ))
        : null}
    </section>
  );
}
