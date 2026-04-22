export type InlineStatusCardState = "input_required" | "auth_required";

type InlineStatusCardProps = {
  state: InlineStatusCardState;
  detail?: string | null;
};

const statusCopy: Record<InlineStatusCardState, string> = {
  input_required: "当前 turn 正在等待你的补充输入，补充后可继续执行。",
  auth_required: "当前 turn 需要你的审批确认，确认后才会继续执行。",
};

export function InlineStatusCard({
  state,
  detail = null,
}: InlineStatusCardProps) {
  return (
    <section className="inline-status-card" role="status" aria-live="polite">
      <strong>{state === "auth_required" ? "Approval required" : "Input required"}</strong>
      <p>{statusCopy[state]}</p>
      {detail ? <p>{detail}</p> : null}
    </section>
  );
}
