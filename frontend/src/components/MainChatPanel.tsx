import { useEffect, useMemo, useRef, useState } from "react";

import type {
  ApprovalTask,
  ChatMessage,
  MainChatPanelProps,
  MessageRole,
  ToolExecutionRecord,
} from "../ui-contracts";
import { ApprovalToolBubble } from "./ApprovalToolBubble";
import { ToolExecutionBubble } from "./ToolExecutionBubble";

const ROLE_HINT: Partial<Record<MessageRole, string>> = {
  user: "you",
  reasoning: "thinking",
  tool: "tool",
  system: "system",
};

function MessageBubble({ message }: { message: ChatMessage }) {
  const hint = ROLE_HINT[message.role];
  if (message.role === "tool") {
    return (
      <div className="msg msg--tool-centered">
        <div className="msg__body msg__body--wide">
          {hint ? <div className="msg__hint">{hint}</div> : null}
          <div className="msg__bubble msg__bubble--tool-centered">{message.content}</div>
        </div>
      </div>
    );
  }

  return (
    <div className={`msg msg--${message.role}`}>
      <div className="msg__body">
        {hint ? <div className="msg__hint">{hint}</div> : null}
        <div className="msg__bubble">{message.content}</div>
      </div>
    </div>
  );
}

type StreamItem =
  | { key: string; ts: number; kind: "message"; message: ChatMessage }
  | { key: string; ts: number; kind: "tool_execution"; execution: ToolExecutionRecord }
  | {
      key: string;
      ts: number;
      kind: "approval";
      task: ApprovalTask;
    };

function buildStream(
  messages: ChatMessage[],
  approvals: ApprovalTask[],
  toolExecutions: ToolExecutionRecord[],
): StreamItem[] {
  const items: StreamItem[] = [];
  for (const m of messages) {
    if (!String(m.content ?? "").trim()) {
      continue;
    }
    items.push({ key: `m:${m.id}`, ts: m.createdAt, kind: "message", message: m });
  }
  for (const execution of toolExecutions) {
    items.push({
      key: `x:${execution.id}`,
      ts: execution.createdAt,
      kind: "tool_execution",
      execution,
    });
  }
  for (const t of approvals) {
    items.push({ key: `a:${t.id}`, ts: t.createdAt, kind: "approval", task: t });
  }
  items.sort((a, b) => a.ts - b.ts);
  return items;
}

export function MainChatPanel({
  messages,
  approvals = [],
  toolExecutions = [],
  submittingToolCallIds,
  runningToolCallIds,
  completedToolCallIds,
  disabled,
  sending,
  onSendMessage,
  onDecideToolCall,
}: MainChatPanelProps) {
  const [input, setInput] = useState("");
  const streamRef = useRef<HTMLDivElement | null>(null);

  const stream = useMemo(
    () => buildStream(messages, approvals, toolExecutions),
    [messages, approvals, toolExecutions],
  );

  useEffect(() => {
    const el = streamRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  }, [stream.length]);

  const onSubmit = async () => {
    const content = input.trim();
    if (!content || disabled || sending) {
      return;
    }
    await onSendMessage(content);
    setInput("");
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.ctrlKey) {
      event.preventDefault();
      void onSubmit();
    } else {
      // Ctrl+Enter 保持 textarea 默认行为（插入换行）。
    }
  };

  const pendingApprovalCount = approvals.reduce((acc, task) => {
    if (task.handled) {
      return acc;
    }
    return acc + task.payload.args.tool_calls.length;
  }, 0);

  return (
    <section className="panel panel--flex chat">
      <header className="chat__header">
        <div className="chat__title">
          <span className="chat__title-main">主对话</span>
          <span className="chat__title-sub">当前对话</span>
        </div>
        <div className="chat__header-meta">
          {pendingApprovalCount > 0 && (
            <span className="pill pill--warn">{pendingApprovalCount} 待审批</span>
          )}
          <span className="pill">{messages.length} 条消息</span>
        </div>
      </header>

      <div ref={streamRef} className="chat__stream">
        {stream.length === 0 ? (
          <div className="chat__empty">开始输入，与 Agent 对话吧</div>
        ) : (
          stream.map((item) => {
            if (item.kind === "message") {
              return <MessageBubble key={item.key} message={item.message} />;
            }
            if (item.kind === "tool_execution") {
              return <ToolExecutionBubble key={item.key} item={item.execution} />;
            }
            return (
              <ApprovalToolBubble
                key={item.key}
                task={item.task}
                submittingToolCallIds={submittingToolCallIds}
                runningToolCallIds={runningToolCallIds}
                completedToolCallIds={completedToolCallIds}
                onDecide={onDecideToolCall}
              />
            );
          })
        )}
      </div>

      <div className="chat__composer">
        <div className="chat__composer-input">
          <textarea
            className="chat__textarea"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            rows={2}
            placeholder="输入消息（Enter 发送，Ctrl+Enter 换行）"
            disabled={disabled || sending}
          />
        </div>
        <button
          type="button"
          className="btn btn--primary"
          onClick={() => void onSubmit()}
          disabled={disabled || sending || !input.trim()}
        >
          {sending ? "发送中…" : "发送"}
        </button>
      </div>
    </section>
  );
}
