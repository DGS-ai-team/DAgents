import type { SubAgentThreadTabsProps } from "../ui-contracts";
import { SubAgentStatusPill } from "./ui";

export function SubAgentThreadTabs({
  threads,
  activeThreadId,
  onSwitchThread,
}: SubAgentThreadTabsProps) {
  if (threads.length === 0) {
    return <div className="thread__empty">暂无子 Agent 线程</div>;
  }

  return (
    <div className="tabs">
      {threads.map((thread) => {
        const isActive = thread.id === activeThreadId;
        return (
          <button
            key={thread.id}
            type="button"
            className={`tab${isActive ? " tab--active" : ""}`}
            onClick={() => onSwitchThread(thread.id)}
          >
            <span>{thread.title || thread.agentId}</span>
            <SubAgentStatusPill status={thread.status} />
          </button>
        );
      })}
    </div>
  );
}
