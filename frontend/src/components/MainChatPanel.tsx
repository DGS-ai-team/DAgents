import { useEffect, useMemo, useRef, useState } from "react";

import type {
  ApprovalTask,
  ChatMessage,
  MainChatPanelProps,
  MessageRole,
} from "../ui-contracts";
import { ApprovalToolBubble } from "./ApprovalToolBubble";
import { initialsOf } from "./ui";

const ROLE_LABEL: Record<MessageRole, string> = {
  user: "你",
  assistant: "助手",
  tool: "工具",
  reasoning: "推理",
  system: "系统",
};

const ROLE_INITIALS: Record<MessageRole, string> = {
  user: "我",
  assistant: "AI",
  tool: "TL",
  reasoning: "R",
  system: "S",
};

function MessageBubble({ message }: { message: ChatMessage }) {
  if (message.role === "tool") {
    return (
      <div className="msg msg--tool-centered">
        <div className="msg__body msg__body--wide">
          <span className="msg__role">{ROLE_LABEL[message.role] ?? message.role}</span>
          <div className="msg__bubble msg__bubble--tool-centered">{message.content}</div>
        </div>
      </div>
    );
  }

  const initials = ROLE_INITIALS[message.role] ?? initialsOf(message.role);
  return (
    <div className={`msg msg--${message.role}`}>
      <div className="msg__avatar">{initials}</div>
      <div className="msg__body">
        <span className="msg__role">{ROLE_LABEL[message.role] ?? message.role}</span>
        <div className="msg__bubble">{message.content}</div>
      </div>
    </div>
  );
}

type StreamItem =
  | { key: string; ts: number; kind: "message"; message: ChatMessage }
  | {
      key: string;
      ts: number;
      kind: "approval";
      task: ApprovalTask;
    };

function buildStream(
  messages: ChatMessage[],
  approvals: ApprovalTask[],
): StreamItem[] {
  const items: StreamItem[] = [];
  for (const m of messages) {
    items.push({ key: `m:${m.id}`, ts: m.createdAt, kind: "message", message: m });
  }
  for (const t of approvals) {
    items.push({ key: `a:${t.id}`, ts: t.createdAt, kind: "approval", task: t });
  }
  items.sort((a, b) => a.ts - b.ts);
  return items;
}

export function MainChatPanel({
  sessionId,
  messages,
  approvals = [],
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
    () => buildStream(messages, approvals),
    [messages, approvals],
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
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void onSubmit();
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
          <span className="chat__title-sub">会话 {sessionId || "(未选择)"}</span>
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
        <textarea
          className="chat__textarea"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          rows={2}
          placeholder="输入消息（⌘/Ctrl + Enter 发送）"
          disabled={disabled || sending}
        />
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
