import { useState } from "react";

type ComposerProps = {
  waitingForInput: boolean;
  onSubmit: (message: string) => void;
};

export function Composer({ waitingForInput, onSubmit }: ComposerProps) {
  const [value, setValue] = useState("");

  return (
    <form
      className="composer"
      onSubmit={(event) => {
        event.preventDefault();
        const nextValue = value.trim();
        if (!nextValue) {
          return;
        }

        onSubmit(nextValue);
        setValue("");
      }}
    >
      <textarea
        value={value}
        onChange={(event) => setValue(event.target.value)}
        placeholder={
          waitingForInput
            ? "当前任务在等待你的补充说明..."
            : "给 Axon 发送下一条任务说明..."
        }
      />
      <button type="submit">{waitingForInput ? "Continue" : "Send"}</button>
    </form>
  );
}
