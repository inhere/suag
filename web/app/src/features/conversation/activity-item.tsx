import type { ConversationActivityEntry } from "./conversation-timeline";

type ActivityItemProps = {
  activity: ConversationActivityEntry;
};

export function ActivityItem({ activity }: ActivityItemProps) {
  return (
    <article className={`conversation-activity conversation-activity--${activity.tone ?? "muted"}`}>
      <div className="conversation-activity__rail" aria-hidden="true" />
      <div className="conversation-activity__body">
        <div className="conversation-activity__meta">
          <strong>{activity.title}</strong>
          {activity.timestamp ? <time>{activity.timestamp}</time> : null}
        </div>
        <p>{activity.detail}</p>
      </div>
    </article>
  );
}
