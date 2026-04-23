export type InlineStatusCardState =
  | "input_required"
  | "auth_required"
  | "failed"
  | "canceled"
  | "completed_empty";

type InlineStatusCardProps = {
  state: InlineStatusCardState;
  detail?: string | null;
};

const statusTitle: Record<InlineStatusCardState, string> = {
  input_required: "Input required",
  auth_required: "Approval required",
  failed: "Failed",
  canceled: "Stopped",
  completed_empty: "Completed",
};

const statusCopy: Record<InlineStatusCardState, string> = {
  input_required: "当前 turn 正在等待你的补充输入，补充后可继续执行。",
  auth_required: "当前 turn 需要你的审批确认，确认后才会继续执行。",
  failed: "当前 turn 执行失败，需要查看失败原因后再决定下一步。",
  canceled: "当前 turn 已停止，可以直接发起新的消息继续处理。",
  completed_empty: "当前 turn 已完成，但还没有可展示的 assistant 回复。",
};

export function InlineStatusCard({
  state,
  detail = null,
}: InlineStatusCardProps) {
  return (
    <section className="inline-status-card" role="status" aria-live="polite">
      <strong>{statusTitle[state]}</strong>
      <p>{statusCopy[state]}</p>
      {detail ? <p>{detail}</p> : null}
    </section>
  );
}
