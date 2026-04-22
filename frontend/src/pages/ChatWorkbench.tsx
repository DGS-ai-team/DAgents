import { useEffect, useMemo, useRef, useState } from "react";

import { DAgentsApiClient } from "../api/client";
import { MainChatPanel } from "../components/MainChatPanel";
import { RuntimeStatusPanel } from "../components/RuntimeStatusPanel";
import { SubAgentThreadTabs } from "../components/SubAgentThreadTabs";
import { SubAgentThreadView } from "../components/SubAgentThreadView";
import { RequestStatusPill } from "../components/ui";
import type {
  ApprovalTask,
  ChatMessage,
  RuntimeState,
  SubAgentThread,
  ToolCallDecision,
  ToolCallItem,
} from "../ui-contracts";

const api = new DAgentsApiClient({
  baseUrl: String(import.meta.env.VITE_API_BASE_URL ?? ""),
});

const DEFAULT_SESSION_ID = "s-web";

function createMessage(
  sessionId: string,
  role: ChatMessage["role"],
  content: string,
  requestId?: string,
): ChatMessage {
  return {
    id: `${role}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    sessionId,
    requestId,
    createdAt: Date.now(),
    role,
    content,
  };
}

export function ChatWorkbench() {
  const [sessionId, setSessionId] = useState<string>(DEFAULT_SESSION_ID);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [approvals, setApprovals] = useState<ApprovalTask[]>([]);
  const [threads, setThreads] = useState<SubAgentThread[]>([]);
  const [submittingToolCallIds, setSubmittingToolCallIds] = useState<string[]>([]);
  const [runningToolCallIds, setRunningToolCallIds] = useState<string[]>([]);
  const [completedToolCallIds, setCompletedToolCallIds] = useState<string[]>([]);
  const [sending, setSending] = useState(false);
  const [runtime, setRuntime] = useState<RuntimeState>({
    status: "idle",
    usage: {
      inputTokens: 0,
      outputTokens: 0,
      totalTokens: 0,
    },
    model: "gpt-4.1",
  });
  const [latestError, setLatestError] = useState<string | undefined>(undefined);
  const [activeThreadId, setActiveThreadId] = useState<string | undefined>(
    undefined,
  );
  const streamRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let mounted = true;
    void api
      .createSession({ session_id: DEFAULT_SESSION_ID })
      .then((result) => {
        if (mounted) {
          setSessionId(result.session_id);
        } else {
          return;
        }
      })
      .catch((error) => {
        if (mounted) {
          setLatestError(String(error));
          setRuntime((prev) => ({ ...prev, status: "error", errorMessage: String(error) }));
        } else {
          return;
        }
      });

    return () => {
      mounted = false;
      if (streamRef.current) {
        streamRef.current.close();
      } else {
        return;
      }
    };
  }, []);

  const firstPendingCalls = useMemo(() => {
    const first = approvals.find((item) => !item.handled);
    return first?.payload.args.tool_calls ?? [];
  }, [approvals]);

  const activeThread = useMemo(
    () => threads.find((item) => item.id === activeThreadId) || null,
    [activeThreadId, threads],
  );

  const openStream = (requestId: string) => {
    if (streamRef.current) {
      streamRef.current.close();
    } else {
      // no active stream
    }

    const es = new EventSource(api.streamUrl(requestId));
    streamRef.current = es;

    const onEvent = (eventType: string, payload: unknown) => {
      const data = payload as Record<string, unknown>;
      const content = typeof data.content === "string" ? data.content : "";

      if (eventType === "assistant" || eventType === "reasoning") {
        if (content) {
          setMessages((prev) => [
            ...prev,
            createMessage(sessionId, eventType === "assistant" ? "assistant" : "reasoning", content, requestId),
          ]);
        } else {
          return;
        }
      } else if (eventType === "tool_result") {
        const toolName = typeof data.tool_name === "string" ? data.tool_name : "tool";
        const toolCallId = typeof data.tool_call_id === "string" ? data.tool_call_id : "";
        const rejected = Boolean(data.rejected);
        const text = rejected ? `[${toolName}] 已拒绝` : `[${toolName}] ${content}`;
        setMessages((prev) => [...prev, createMessage(sessionId, "tool", text, requestId)]);
        if (toolCallId) {
          setRunningToolCallIds((prev) => prev.filter((id) => id !== toolCallId));
          setCompletedToolCallIds((prev) => (prev.includes(toolCallId) ? prev : [...prev, toolCallId]));
        } else {
          return;
        }
      } else if (eventType === "approval_required") {
        const args = (data.approval_args ?? {}) as { tool_calls?: ToolCallItem[] };
        const toolCalls = Array.isArray(args.tool_calls) ? args.tool_calls : [];
        if (toolCalls.length === 0) {
          return;
        } else {
          const idRaw = typeof data.approval_id === "string" ? data.approval_id : "";
          const approvalId = idRaw || `${requestId}-${Date.now()}`;
          const approvalTask: ApprovalTask = {
            id: approvalId,
            sessionId,
            requestId,
            createdAt: Date.now(),
            payload: {
              message: typeof data.content === "string" ? data.content : "工具调用",
              description: typeof data.description === "string" ? data.description : "",
              args: { tool_calls: toolCalls },
            },
            handled: false,
          };
          setApprovals((prev) => {
            const exists = prev.some((item) => item.id === approvalTask.id);
            if (exists) {
              return prev.map((item) => (item.id === approvalTask.id ? approvalTask : item));
            } else {
              return [...prev, approvalTask];
            }
          });
        }
      } else if (eventType === "usage") {
        const input = Number(data.prompt_tokens ?? 0);
        const output = Number(data.completion_tokens ?? 0);
        const total = Number(data.total_tokens ?? input + output);
        setRuntime((prev) => ({
          ...prev,
          usage: {
            inputTokens: Number.isFinite(input) ? input : prev.usage.inputTokens,
            outputTokens: Number.isFinite(output) ? output : prev.usage.outputTokens,
            totalTokens: Number.isFinite(total) ? total : prev.usage.totalTokens,
          },
        }));
      } else if (eventType === "error") {
        const message = typeof data.message === "string" ? data.message : "运行异常";
        setLatestError(message);
        setRuntime((prev) => ({ ...prev, status: "error", errorMessage: message }));
        setSending(false);
      } else if (eventType === "done") {
        setRuntime((prev) => ({ ...prev, status: "done" }));
        setSending(false);
        setSubmittingToolCallIds([]);
      } else if (eventType === "subagent_started") {
        const subId = String(data.subagent_id ?? "").trim();
        if (!subId) {
          return;
        } else {
          const thread: SubAgentThread = {
            id: subId,
            parentRequestId: requestId,
            sessionId,
            createdAt: Date.now(),
            agentId: subId,
            title: typeof data.title === "string" ? data.title : subId,
            status: "running",
            chunks: [],
            startedAt: Date.now(),
          };
          setThreads((prev) => {
            if (prev.some((item) => item.id === thread.id)) {
              return prev;
            } else {
              return [...prev, thread];
            }
          });
          setActiveThreadId(subId);
        }
      } else if (eventType === "subagent_delta") {
        const subId = String(data.subagent_id ?? "").trim();
        const delta = String(data.content ?? "");
        if (!subId || !delta) {
          return;
        } else {
          setThreads((prev) =>
            prev.map((item) =>
              item.id === subId
                ? {
                    ...item,
                    chunks: [
                      ...item.chunks,
                      {
                        id: `chunk-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
                        kind: "delta",
                        content: delta,
                        ts: Date.now(),
                      },
                    ],
                  }
                : item,
            ),
          );
        }
      } else if (eventType === "subagent_done" || eventType === "subagent_error") {
        const subId = String(data.subagent_id ?? "").trim();
        if (!subId) {
          return;
        } else {
          setThreads((prev) =>
            prev.map((item) =>
              item.id === subId
                ? {
                    ...item,
                    status: eventType === "subagent_done" ? "success" : "error",
                    endedAt: Date.now(),
                    errorMessage: eventType === "subagent_error" ? String(data.message ?? "") : item.errorMessage,
                  }
                : item,
            ),
          );
        }
      } else if (eventType === "tool_call") {
        if (content) {
          setMessages((prev) => [...prev, createMessage(sessionId, "assistant", content, requestId)]);
        } else {
          return;
        }
      } else {
        // ignore unsupported event types
      }
    };

    const parseAndDispatch = (eventType: string, rawData: string) => {
      try {
        const parsed = JSON.parse(rawData) as { data?: unknown };
        const payload = parsed && typeof parsed === "object" && "data" in parsed ? parsed.data : parsed;
        onEvent(eventType, payload);
      } catch {
        // ignore malformed sse payload
      }
    };

    const eventTypes = [
      "assistant",
      "reasoning",
      "tool_call",
      "tool_result",
      "approval_required",
      "usage",
      "error",
      "done",
      "subagent_started",
      "subagent_delta",
      "subagent_done",
      "subagent_error",
    ];
    for (const type of eventTypes) {
      es.addEventListener(type, (event) => {
        const msgEvent = event as MessageEvent;
        parseAndDispatch(type, msgEvent.data);
      });
    }

    es.onerror = () => {
      setSending(false);
      setRuntime((prev) =>
        prev.status === "running" ? { ...prev, status: "error", errorMessage: "SSE 连接异常" } : prev,
      );
    };
  };

  const handleSendMessage = async (content: string) => {
    const text = content.trim();
    if (!text) {
      return;
    } else {
      setLatestError(undefined);
      setMessages((prev) => [...prev, createMessage(sessionId, "user", text)]);
      setSending(true);
      setRuntime((prev) => ({ ...prev, status: "running", errorMessage: undefined }));
      try {
        const result = await api.submitMessage({
          session_id: sessionId,
          request_type: "message",
          content: text,
          source: "frontend",
        });
        openStream(result.request_id);
      } catch (error) {
        const message = String(error);
        setSending(false);
        setLatestError(message);
        setRuntime((prev) => ({ ...prev, status: "error", errorMessage: message }));
      }
    }
  };

  const handleToolDecision = async (
    taskId: string,
    toolCallId: string,
    decision: ToolCallDecision,
  ) => {
    setSubmittingToolCallIds((prev) => (prev.includes(toolCallId) ? prev : [...prev, toolCallId]));
    setLatestError(undefined);

    const task = approvals.find((item) => item.id === taskId);
    if (!task) {
      setSubmittingToolCallIds((prev) => prev.filter((id) => id !== toolCallId));
      return;
    } else {
      const approved = decision === "approve" ? [toolCallId] : [];
      const rejected = decision === "reject" ? [toolCallId] : [];

      try {
        const result = await api.submitResume(sessionId, {
          type: "selection",
          approved,
          rejected,
        });

        setApprovals((prev) =>
          prev.map((item) =>
            item.id === taskId
              ? {
                  ...item,
                  approvedIds: [...(item.approvedIds ?? []), ...approved],
                  rejectedIds: [...(item.rejectedIds ?? []), ...rejected],
                  handled:
                    ((item.approvedIds ?? []).length + (item.rejectedIds ?? []).length + 1) >=
                    item.payload.args.tool_calls.length,
                  decision: "selective",
                  handledAt: Date.now(),
                }
              : item,
          ),
        );

        if (decision === "approve") {
          setRunningToolCallIds((prev) => (prev.includes(toolCallId) ? prev : [...prev, toolCallId]));
        } else {
          setRunningToolCallIds((prev) => prev.filter((id) => id !== toolCallId));
        }

        setSubmittingToolCallIds((prev) => prev.filter((id) => id !== toolCallId));
        setRuntime((prev) => ({ ...prev, status: "running" }));
        openStream(result.request_id);
      } catch (error) {
        const message = String(error);
        setSubmittingToolCallIds((prev) => prev.filter((id) => id !== toolCallId));
        setLatestError(message);
        setRuntime((prev) => ({ ...prev, status: "error", errorMessage: message }));
      }
    }
  };

  return (
    <div className="app">
      <header className="app__header">
        <div className="app__brand">
          <div className="app__brand-mark" />
          <div>
            <div className="app__title">DAgents Workbench</div>
            <div className="app__subtitle">多 Agent 工作台 · MVP</div>
          </div>
        </div>
        <div className="app__meta">
          <span>session: {sessionId || "-"}</span>
          {runtime && <RequestStatusPill status={runtime.status} />}
        </div>
      </header>

      <div className="app__body app__body--two-col">
        <main className="app__col">
          <MainChatPanel
            sessionId={sessionId}
            messages={messages}
            approvals={approvals}
            submittingToolCallIds={submittingToolCallIds}
            runningToolCallIds={runningToolCallIds}
            completedToolCallIds={completedToolCallIds}
            sending={sending}
            onSendMessage={handleSendMessage}
            onDecideToolCall={handleToolDecision}
          />
        </main>

        <aside className="app__col">
          {runtime && (
            <RuntimeStatusPanel
              runtime={runtime}
              latestToolCalls={firstPendingCalls}
              latestError={latestError}
            />
          )}
          <section className="panel">
            <header className="panel__header">
              <div className="panel__title">
                子 Agent 实时线程
                <span className="pill">{threads.length}</span>
              </div>
            </header>
            <div className="panel__body">
              <SubAgentThreadTabs
                threads={threads}
                activeThreadId={activeThreadId}
                onSwitchThread={setActiveThreadId}
              />
              <SubAgentThreadView thread={activeThread} />
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}
