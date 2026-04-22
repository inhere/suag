export type SummaryStripTone = "running" | "waiting" | "attention" | "failed";

type SummaryStripProps = {
  status: string;
  tone: SummaryStripTone;
};

export function SummaryStrip({ status, tone }: SummaryStripProps) {
  return (
    <section className={`summary-strip summary-strip--${tone}`} role="status" aria-live="polite">
      <span>{status}</span>
    </section>
  );
}
