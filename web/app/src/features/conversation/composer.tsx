import { useState } from "react";

type ComposerProps = {
  waitingForInput: boolean;
  submitPending?: boolean;
  submitError?: string | null;
  onSubmit: (message: string) => void;
};

export function Composer({
  waitingForInput,
  submitPending = false,
  submitError = null,
  onSubmit,
}: ComposerProps) {
  const [value, setValue] = useState("");
  const submitLabel = submitPending
    ? waitingForInput
      ? "Continuing..."
      : "Starting..."
    : waitingForInput
      ? "Continue"
      : "Send";
  const statusMessage = submitPending
    ? waitingForInput
      ? "正在继续当前 turn..."
      : "正在创建新 turn..."
    : "";

  return (
    <form
      className="composer"
      onSubmit={(event) => {
        event.preventDefault();
        const nextValue = value.trim();
        if (!nextValue || submitPending) {
          return;
        }

        onSubmit(nextValue);
        setValue("");
      }}
    >
      <textarea
        className="composer__input"
        value={value}
        onChange={(event) => setValue(event.target.value)}
        disabled={submitPending}
        placeholder={
          waitingForInput
            ? "当前任务在等待你的补充说明..."
            : "给 Axon 发送下一条任务说明..."
        }
      />
      <div className="composer__footer">
        {statusMessage ? <span className="action-status composer__status">{statusMessage}</span> : <span />}
        <button className="composer__submit" type="submit" disabled={submitPending || !value.trim()}>
          {submitLabel}
        </button>
      </div>
      {submitError ? <p className="inline-feedback inline-feedback--error">{submitError}</p> : null}
    </form>
  );
}
