import type { RuntimeStatusPanelProps } from "../ui-contracts";
import { RequestStatusPill, formatNumber } from "./ui";

export function RuntimeStatusPanel({
  runtime,
  latestToolCalls,
  latestError,
}: RuntimeStatusPanelProps) {
  const errorText = latestError || runtime.errorMessage;

  return (
    <section className="panel">
      <header className="panel__header">
        <div className="panel__title">运行状态</div>
        <RequestStatusPill status={runtime.status} />
      </header>
      <div className="panel__body">
        <div className="runtime__row">
          <span className="runtime__label">模型</span>
          <span className="runtime__value">{runtime.model || "-"}</span>
        </div>
        <div className="runtime__row">
          <span className="runtime__label">最近工具调用</span>
          <span className="runtime__value">{latestToolCalls.length}</span>
        </div>

        <div className="stats">
          <div className="stat">
            <span className="stat__label">Input</span>
            <span className="stat__value">{formatNumber(runtime.usage.inputTokens)}</span>
          </div>
          <div className="stat">
            <span className="stat__label">Output</span>
            <span className="stat__value">{formatNumber(runtime.usage.outputTokens)}</span>
          </div>
          <div className="stat stat--wide">
            <span className="stat__label">Total tokens</span>
            <span className="stat__value">{formatNumber(runtime.usage.totalTokens)}</span>
          </div>
        </div>

        {errorText && <div className="runtime__error">{errorText}</div>}
      </div>
    </section>
  );
}
