import type { ApprovalTask, ToolCallDecision, ToolCallItem } from "../ui-contracts";
import { RiskBadge } from "./ui";

export interface ApprovalToolBubbleProps {
  task: ApprovalTask;
  submittingToolCallIds?: string[];
  runningToolCallIds?: string[];
  completedToolCallIds?: string[];
  onDecide?: (
    taskId: string,
    toolCallId: string,
    decision: ToolCallDecision,
  ) => Promise<void>;
}

function decisionForToolCall(
  task: ApprovalTask,
  toolCallId: string,
): ToolCallDecision | null {
  if (task.decision === "approve_all") {
    return "approve";
  }
  if (task.decision === "reject_all") {
    return "reject";
  }
  if (task.approvedIds?.includes(toolCallId)) {
    return "approve";
  }
  if (task.rejectedIds?.includes(toolCallId)) {
    return "reject";
  }
  if (!task.handled) {
    return null;
  }
  return null;
}

function ToolStatusPill({
  decision,
  running,
  completed,
}: {
  decision: ToolCallDecision | null;
  running: boolean;
  completed: boolean;
}) {
  if (decision === "reject") {
    return <span className="pill pill--error">已拒绝</span>;
  }
  if (completed) {
    return <span className="pill pill--success">已返回</span>;
  }
  if (running) {
    return <span className="pill pill--running">执行中</span>;
  }
  if (decision === "approve") {
    return <span className="pill">已批准</span>;
  }
  return <span className="pill pill--warn">待审批</span>;
}

function ToolCallRow({
  task,
  toolCall,
  submitting,
  running,
  completed,
  onDecide,
}: {
  task: ApprovalTask;
  toolCall: ToolCallItem;
  submitting: boolean;
  running: boolean;
  completed: boolean;
  onDecide?: (
    taskId: string,
    toolCallId: string,
    decision: ToolCallDecision,
  ) => Promise<void>;
}) {
  const decision = decisionForToolCall(task, toolCall.id);
  const handled = decision !== null;

  return (
    <li className="approval-tool-item">
      <header className="approval-tool-item__head">
        <div className="approval-bubble__title">
          <span className="approval-bubble__name">{toolCall.name}</span>
          <RiskBadge level={toolCall.riskLevel} />
        </div>
        <ToolStatusPill decision={decision} running={running} completed={completed} />
      </header>
      <div className="approval-bubble__id">call_id: {toolCall.id}</div>

      <pre className="tool-card__args tool-card__args--compact">
        {JSON.stringify(toolCall.arguments, null, 2)}
      </pre>

      {!handled && onDecide && (
        <footer className="approval-bubble__actions">
          <button
            type="button"
            className="btn btn--sm btn--danger"
            disabled={submitting}
            onClick={() => void onDecide(task.id, toolCall.id, "reject")}
          >
            拒绝
          </button>
          <button
            type="button"
            className="btn btn--sm btn--primary"
            disabled={submitting}
            onClick={() => void onDecide(task.id, toolCall.id, "approve")}
          >
            {submitting ? "处理中…" : "批准"}
          </button>
        </footer>
      )}
    </li>
  );
}

export function ApprovalToolBubble({
  task,
  submittingToolCallIds,
  runningToolCallIds,
  completedToolCallIds,
  onDecide,
}: ApprovalToolBubbleProps) {
  const submittingSet = new Set(submittingToolCallIds ?? []);
  const runningSet = new Set(runningToolCallIds ?? []);
  const completedSet = new Set(completedToolCallIds ?? []);

  return (
    <div className="msg msg--approval">
      <div className="msg__body msg__body--wide">
        <div className="approval-bubble">
          <div className="approval-bubble__intro">工具调用</div>
          <ul className="approval-tool-list">
            {task.payload.args.tool_calls.map((toolCall) => (
              <ToolCallRow
                key={toolCall.id}
                task={task}
                toolCall={toolCall}
                submitting={submittingSet.has(toolCall.id)}
                running={runningSet.has(toolCall.id) && !completedSet.has(toolCall.id)}
                completed={completedSet.has(toolCall.id)}
                onDecide={onDecide}
              />
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}
