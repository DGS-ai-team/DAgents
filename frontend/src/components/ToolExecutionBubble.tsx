import type { ToolExecutionRecord } from "../ui-contracts";

export function ToolExecutionBubble({ item }: { item: ToolExecutionRecord }) {
  const isRunning = item.status === "running";
  const isSuccess = item.status === "success";
  const isRejected = item.status === "rejected";
  const isError = item.status === "error";
  const statusText = isRunning
    ? "执行中"
    : isSuccess
      ? "已完成"
      : isRejected
        ? "已拒绝"
        : "失败";
  const summary = (item.summary ?? "").trim() || "无摘要";

  return (
    <div className="msg msg--tool-centered">
      <div className="msg__body msg__body--wide">
        <div className="msg__hint">tool_call</div>
        <div className="tool-exec-bubble">
          <div className="tool-exec-bubble__head">
            <span className="tool-exec-bubble__name">{item.toolName}</span>
            <span className="tool-exec-bubble__status">
              {isRunning ? (
                <span className="tool-exec-spinner" aria-hidden="true" />
              ) : (
                <span
                  className={`tool-exec-status-icon ${isSuccess ? "tool-exec-status-icon--success" : ""} ${isRejected ? "tool-exec-status-icon--rejected" : ""} ${isError ? "tool-exec-status-icon--error" : ""}`}
                  aria-hidden="true"
                >
                  {isSuccess ? "✓" : isRejected ? "−" : "!"}
                </span>
              )}
              <span>{statusText}</span>
            </span>
          </div>

          <div className="tool-exec-bubble__summary">{summary}</div>

          <details className="tool-exec-bubble__details">
            <summary>查看详情</summary>
            <pre className="tool-card__args tool-card__args--compact">
              {JSON.stringify(item.arguments, null, 2)}
            </pre>
            {item.detail ? (
              <pre className="tool-card__args tool-card__args--compact">
                {item.detail}
              </pre>
            ) : null}
          </details>
        </div>
      </div>
    </div>
  );
}
