import type { ApprovalTask, ToolCallDecision, ToolCallItem } from "../ui-contracts";

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
    return <span className="tool-status-chip tool-status-chip--reject">已拒绝</span>;
  }
  if (completed) {
    return <span className="tool-status-chip tool-status-chip--done">已返回</span>;
  }
  if (running) {
    return <span className="tool-status-chip tool-status-chip--running">执行中</span>;
  }
  if (decision === "approve") {
    return <span className="tool-status-chip tool-status-chip--approved">已批准</span>;
  }
  return <span className="tool-status-chip tool-status-chip--pending">待审批</span>;
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
        </div>
        <div className="approval-tool-item__right">
          <ToolStatusPill decision={decision} running={running} completed={completed} />
          {!handled && onDecide ? (
            <div className="approval-tool-item__inline-actions">
              <button
                type="button"
                className="approval-action-btn approval-action-btn--reject"
                disabled={submitting}
                onClick={() => void onDecide(task.id, toolCall.id, "reject")}
              >
                拒绝
              </button>
              <button
                type="button"
                className="approval-action-btn approval-action-btn--approve"
                disabled={submitting}
                onClick={() => void onDecide(task.id, toolCall.id, "approve")}
              >
                {submitting ? "处理中…" : "批准"}
              </button>
            </div>
          ) : null}
        </div>
      </header>

      <pre className="tool-card__args tool-card__args--compact">
        {JSON.stringify(toolCall.arguments, null, 2)}
      </pre>
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
        <div className="msg__hint">tool_call</div>
        <div className="approval-bubble">
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
