import type { RuntimeStatusPanelProps } from "../ui-contracts";
import { formatNumber } from "./ui";

export function RuntimeStatusPanel({
  runtime,
  latestError,
  sseConnected,
}: RuntimeStatusPanelProps) {
  const errorText = latestError || runtime.errorMessage;

  return (
    <section className="panel runtime-panel--compact">
      <header className="panel__header">
        <div className="panel__title">运行状态</div>
      </header>
      <div className="panel__body runtime-panel__body--compact">
        <div className="runtime-compact-grid">
          <div className="runtime-compact-card">
            <span className="runtime-compact-card__label">Total tokens</span>
            <span className="runtime-compact-card__value">{formatNumber(runtime.usage.totalTokens)}</span>
          </div>
          <div className="runtime-compact-card">
            <span className="runtime-compact-card__label">SSE 连接</span>
            <span
              className={`runtime-sse-status ${sseConnected ? "runtime-sse-status--online" : "runtime-sse-status--offline"}`}
            >
              {sseConnected ? "已连接" : "已断开"}
            </span>
          </div>
        </div>

        {errorText && <div className="runtime__error">{errorText}</div>}
      </div>
    </section>
  );
}
