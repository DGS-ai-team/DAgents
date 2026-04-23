import type { ReactNode } from "react";
import type { ToolExecutionRecord } from "../ui-contracts";

function renderByDisplayType(item: ToolExecutionRecord): ReactNode {
  const displayType = item.displayType ?? "normal_text";
  const summary = (item.summary ?? "").trim();
  const resultContent = (item.resultContent ?? "").trim();
  const displayText = resultContent || summary;
  if (!displayText) {
    return null;
  }
  if (displayType === "terminal") {
    return <pre className="tool-exec-bubble__terminal">{displayText}</pre>;
  }
  if (displayType === "code") {
    return <pre className="tool-exec-bubble__code">{displayText}</pre>;
  }
  if (displayType === "image") {
    const imageUrl = displayText;
    return <img src={imageUrl} alt="tool result" className="tool-exec-bubble__image" />;
  }
  return <div className="tool-exec-bubble__summary">{summary || displayText}</div>;
}

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
  const summaryNode = renderByDisplayType(item);

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

          {summaryNode ?? <div className="tool-exec-bubble__summary">无摘要</div>}

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
