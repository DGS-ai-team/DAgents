import type { SubAgentThreadTabsProps } from "../ui-contracts";
import { SubAgentStatusPill } from "./ui";

function IconThread() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="thread-tab__icon">
      <path d="M4 6h16M4 12h11M4 18h8" />
    </svg>
  );
}

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
            <IconThread />
            <span>{thread.title || thread.agentId}</span>
            <SubAgentStatusPill status={thread.status} />
          </button>
        );
      })}
    </div>
  );
}
