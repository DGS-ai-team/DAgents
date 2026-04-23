import { useEffect, useMemo, useRef, useState } from "react";

import { DAgentsApiClient } from "../api/client";
import { MainChatPanel } from "../components/MainChatPanel";
import { RuntimeStatusPanel } from "../components/RuntimeStatusPanel";
import { SubAgentThreadTabs } from "../components/SubAgentThreadTabs";
import { SubAgentThreadView } from "../components/SubAgentThreadView";
import type {
  ApprovalTask,
  ChatMessage,
  RuntimeState,
  SubAgentThread,
  ToolExecutionRecord,
  ToolCallDecision,
  ToolCallItem,
} from "../ui-contracts";

const resolvedApiBaseUrl = String(import.meta.env.VITE_API_BASE_URL ?? "").trim();
const api = new DAgentsApiClient({
  baseUrl: resolvedApiBaseUrl || "http://127.0.0.1:8000",
});

const DEFAULT_SESSION_ID = "s-web";

function wbLog(message: string, payload?: unknown): void {
  const now = new Date().toISOString();
  if (payload === undefined) {
    console.log(`[ChatWorkbench] ${now} ${message}`);
  } else {
    console.log(`[ChatWorkbench] ${now} ${message}`, payload);
  }
}

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

function buildToolExecutionSummary(
  toolName: string,
  status: "running" | "success" | "rejected" | "error",
  rawText?: string,
): string {
  const name = (toolName || "工具").trim();
  if (status === "running") {
    return `${name} 正在执行`;
  }
  if (status === "rejected") {
    return `${name} 已拒绝`;
  }
  if (status === "error") {
    return `${name} 执行失败`;
  }
  const text = String(rawText ?? "").trim();
  if (!text) {
    return `${name} 已完成`;
  }
  const compact = text.replace(/\s+/g, " ");
  const clipped = compact.length > 56 ? `${compact.slice(0, 56)}...` : compact;
  return `${name}：${clipped}`;
}

function IconPlus() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="session-action-icon">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

function IconEdit() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="session-action-icon">
      <path d="M4 20l4.5-1 9.2-9.2a1.7 1.7 0 0 0 0-2.4l-1.1-1.1a1.7 1.7 0 0 0-2.4 0L5 15.5 4 20z" />
      <path d="M13.5 6.5l4 4" />
    </svg>
  );
}

function IconTrash() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="session-action-icon">
      <path d="M4 7h16" />
      <path d="M9 7V5h6v2" />
      <path d="M7 7l1 12h8l1-12" />
      <path d="M10 11v6M14 11v6" />
    </svg>
  );
}

export function ChatWorkbench() {
  const [clientId] = useState<string>(() => {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID();
    } else {
      return `client-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    }
  });
  const [sessionIds, setSessionIds] = useState<string[]>([]);
  const [activeSessionId, setActiveSessionId] = useState<string>("");
  const [messagesBySession, setMessagesBySession] = useState<Record<string, ChatMessage[]>>({});
  const [approvalsBySession, setApprovalsBySession] = useState<Record<string, ApprovalTask[]>>({});
  const [toolExecutionsBySession, setToolExecutionsBySession] = useState<Record<string, ToolExecutionRecord[]>>(
    {},
  );
  const [threadsBySession, setThreadsBySession] = useState<Record<string, SubAgentThread[]>>({});
  const [activeThreadBySession, setActiveThreadBySession] = useState<Record<string, string | undefined>>({});
  const [submittingToolCallIdsBySession, setSubmittingToolCallIdsBySession] = useState<Record<string, string[]>>(
    {},
  );
  const [runningToolCallIdsBySession, setRunningToolCallIdsBySession] = useState<Record<string, string[]>>({});
  const [completedToolCallIdsBySession, setCompletedToolCallIdsBySession] = useState<Record<string, string[]>>(
    {},
  );
  const [sendingBySession, setSendingBySession] = useState<Record<string, boolean>>({});
  const [runtimeBySession, setRuntimeBySession] = useState<Record<string, RuntimeState>>({});
  const [defaultRuntimeModel] = useState<RuntimeState>({
    status: "idle",
    usage: {
      inputTokens: 0,
      outputTokens: 0,
      totalTokens: 0,
    },
    model: "gpt-4.1",
  });
  const [latestErrorBySession, setLatestErrorBySession] = useState<Record<string, string | undefined>>({});
  const [sessionTitleById, setSessionTitleById] = useState<Record<string, string>>({});
  const [editingSessionId, setEditingSessionId] = useState<string>("");
  const [editingTitleDraft, setEditingTitleDraft] = useState<string>("");
  const [sseConnected, setSseConnected] = useState(false);
  const globalStreamRef = useRef<EventSource | null>(null);
  const seenEventSeqRef = useRef<Set<string>>(new Set());
  const streamTurnBySessionRef = useRef<Record<string, number>>({});

  const activeMessages = messagesBySession[activeSessionId] ?? [];
  const activeApprovals = approvalsBySession[activeSessionId] ?? [];
  const activeToolExecutions = toolExecutionsBySession[activeSessionId] ?? [];
  const activeThreads = threadsBySession[activeSessionId] ?? [];
  const activeThreadId = activeThreadBySession[activeSessionId];
  const activeRuntime = runtimeBySession[activeSessionId] ?? defaultRuntimeModel;
  const activeLatestError = latestErrorBySession[activeSessionId];
  const activeSubmittingToolCallIds = submittingToolCallIdsBySession[activeSessionId] ?? [];
  const activeRunningToolCallIds = runningToolCallIdsBySession[activeSessionId] ?? [];
  const activeCompletedToolCallIds = completedToolCallIdsBySession[activeSessionId] ?? [];
  const activeSending = sendingBySession[activeSessionId] ?? false;
  const sessionHistory = useMemo(() => {
    const hasDefaultSession = sessionIds.includes(DEFAULT_SESSION_ID);
    const others = sessionIds.filter((sid) => sid !== DEFAULT_SESSION_ID).reverse();
    if (hasDefaultSession) {
      return [DEFAULT_SESSION_ID, ...others];
    } else {
      return others;
    }
  }, [sessionIds]);

  const getSessionTitle = (sid: string): string => {
    const custom = (sessionTitleById[sid] ?? "").trim();
    if (custom) {
      return custom;
    } else {
      // no custom title
    }
    if (sid === DEFAULT_SESSION_ID) {
      return "默认对话";
    } else {
      const order = sessionIds.findIndex((item) => item === sid);
      const displayOrder = order >= 0 ? order + 1 : 0;
      return displayOrder > 0 ? `对话 ${displayOrder}` : "对话";
    }
  };

  const ensureSessionSlot = (sid: string) => {
    setSessionIds((prev) => (prev.includes(sid) ? prev : [...prev, sid]));
    setSessionTitleById((prev) => {
      if (sid in prev) {
        return prev;
      } else if (sid === DEFAULT_SESSION_ID) {
        return { ...prev, [sid]: "默认对话" };
      } else {
        return { ...prev, [sid]: "新对话" };
      }
    });
    setRuntimeBySession((prev) => {
      if (sid in prev) {
        return prev;
      } else {
        return {
          ...prev,
          [sid]: {
            status: "idle",
            usage: { inputTokens: 0, outputTokens: 0, totalTokens: 0 },
            model: defaultRuntimeModel.model,
          },
        };
      }
    });
    if (!activeSessionId) {
      setActiveSessionId(sid);
    } else {
      // keep current active tab
    }
  };

  const appendMessageForSession = (sid: string, message: ChatMessage) => {
    setMessagesBySession((prev) => {
      const current = prev[sid] ?? [];
      return { ...prev, [sid]: [...current, message] };
    });
  };

  const upsertToolExecutionForSession = (sid: string, item: ToolExecutionRecord) => {
    setToolExecutionsBySession((prev) => {
      const current = prev[sid] ?? [];
      const index = current.findIndex((row) => row.id === item.id);
      if (index < 0) {
        return { ...prev, [sid]: [...current, item] };
      } else {
        const next = [...current];
        next[index] = item;
        return { ...prev, [sid]: next };
      }
    });
  };

  const handleDeleteSession = (sid: string) => {
    if (sid === DEFAULT_SESSION_ID) {
      wbLog("session:delete:blocked-default", { sessionId: sid });
      return;
    } else {
      // non-default sessions can be deleted
    }
    wbLog("session:delete:start", { sessionId: sid });
    setSessionIds((prev) => {
      const next = prev.filter((item) => item !== sid);
      setActiveSessionId((current) => {
        if (current === sid) {
          return next[0] ?? "";
        } else {
          return current;
        }
      });
      return next;
    });
    setSessionTitleById((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setMessagesBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setApprovalsBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setToolExecutionsBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setThreadsBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setActiveThreadBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setSubmittingToolCallIdsBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setRunningToolCallIdsBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setCompletedToolCallIdsBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setSendingBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setRuntimeBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    setLatestErrorBySession((prev) => {
      const next = { ...prev };
      delete next[sid];
      return next;
    });
    if (editingSessionId === sid) {
      setEditingSessionId("");
      setEditingTitleDraft("");
    } else {
      // keep current editor state
    }
    wbLog("session:delete:done", { sessionId: sid });
  };

  const handleStartEditSessionTitle = (sid: string) => {
    setEditingSessionId(sid);
    setEditingTitleDraft(getSessionTitle(sid));
  };

  const handleCommitEditSessionTitle = (sid: string) => {
    const nextTitle = editingTitleDraft.trim();
    if (!nextTitle) {
      setEditingSessionId("");
      setEditingTitleDraft("");
      return;
    } else {
      setSessionTitleById((prev) => ({ ...prev, [sid]: nextTitle }));
      setEditingSessionId("");
      setEditingTitleDraft("");
    }
  };

  useEffect(() => {
    wbLog("bootstrap:start", {
      configuredApiBaseUrl: resolvedApiBaseUrl,
      effectiveApiBaseUrl: resolvedApiBaseUrl || "http://127.0.0.1:8000",
    });
    let mounted = true;
    void api
      .createSession({ session_id: DEFAULT_SESSION_ID })
      .then((result) => {
        if (mounted) {
          wbLog("bootstrap:createSession:success", result);
          ensureSessionSlot(result.session_id);
        } else {
          return;
        }
      })
      .catch((error) => {
        if (mounted) {
          const message = String(error);
          wbLog("bootstrap:createSession:error", { error: message });
          setLatestErrorBySession((prev) => ({ ...prev, [DEFAULT_SESSION_ID]: message }));
          setRuntimeBySession((prev) => ({
            ...prev,
            [DEFAULT_SESSION_ID]: {
              ...(prev[DEFAULT_SESSION_ID] ?? defaultRuntimeModel),
              status: "error",
              errorMessage: message,
            },
          }));
        } else {
          return;
        }
      });
    if (!globalStreamRef.current) {
      const streamUrl = api.streamAllUrl(clientId);
      wbLog("sse:global:open", { streamUrl, clientId });
      const es = new EventSource(streamUrl);
      globalStreamRef.current = es;
      setSseConnected(true);
    } else {
      wbLog("sse:global:reuse");
      setSseConnected(true);
    }

    return () => {
      mounted = false;
      if (globalStreamRef.current) {
        wbLog("sse:cleanup:close-global");
        globalStreamRef.current.close();
        globalStreamRef.current = null;
        setSseConnected(false);
      } else {
        return;
      }
    };
  }, [clientId]);

  const activeThread = useMemo(
    () => activeThreads.find((item) => item.id === activeThreadId) || null,
    [activeThreadId, activeThreads],
  );

  const appendStreamingMessage = (
    sid: string,
    role: ChatMessage["role"],
    requestId: string,
    deltaText: string,
  ) => {
    // 同一 requestId + 同一 role 的流式片段合并进最后一个气泡，避免每个 delta 生成独立消息。
    setMessagesBySession((prev) => {
      const sessionMessages = prev[sid] ?? [];
      const lastIndex = sessionMessages.length - 1;
      if (lastIndex >= 0) {
        const last = sessionMessages[lastIndex];
        if (last.role === role && last.requestId === requestId && last.sessionId === sid) {
          const merged: ChatMessage = {
            ...last,
            content: `${last.content}${deltaText}`,
          };
          return {
            ...prev,
            [sid]: [...sessionMessages.slice(0, lastIndex), merged],
          };
        } else {
          return {
            ...prev,
            [sid]: [...sessionMessages, createMessage(sid, role, deltaText, requestId)],
          };
        }
      } else {
        return {
          ...prev,
          [sid]: [createMessage(sid, role, deltaText, requestId)],
        };
      }
    });
  };

  useEffect(() => {
    const es = globalStreamRef.current;
    if (!es) {
      return;
    } else {
      // stream exists
    }

    const onEvent = (eventType: string, envelope: Record<string, unknown>) => {
      const sid = String(envelope.session_id ?? "").trim();
      const turnIndex = streamTurnBySessionRef.current[sid] ?? 0;
      const requestId = `${sid}:turn:${turnIndex}`;
      const payload = (envelope.data ?? {}) as Record<string, unknown>;
      const content = typeof payload.content === "string" ? payload.content : "";
      if (!sid) {
        wbLog("sse:event:missing-session-or-request", { eventType, envelope });
        return;
      } else {
        ensureSessionSlot(sid);
      }
      wbLog("sse:event", { eventType, sessionId: sid, requestId, hasContent: Boolean(content) });

      if (eventType === "assistant" || eventType === "reasoning") {
        if (content) {
          appendStreamingMessage(sid, eventType === "assistant" ? "assistant" : "reasoning", requestId, content);
        } else {
          return;
        }
      } else if (eventType === "tool_result") {
        const toolName = typeof payload.tool_name === "string" ? payload.tool_name : "tool";
        const toolCallId = typeof payload.tool_call_id === "string" ? payload.tool_call_id : "";
        const rejected = Boolean(payload.rejected);
        if (!toolCallId) {
          return;
        } else {
          setRunningToolCallIdsBySession((prev) => ({
            ...prev,
            [sid]: (prev[sid] ?? []).filter((id) => id !== toolCallId),
          }));
          setCompletedToolCallIdsBySession((prev) => {
            const current = prev[sid] ?? [];
            return { ...prev, [sid]: current.includes(toolCallId) ? current : [...current, toolCallId] };
          });
          setToolExecutionsBySession((prev) => {
            const current = prev[sid] ?? [];
            const idx = current.findIndex((row) => row.toolCallId === toolCallId);
            if (idx < 0) {
              const created: ToolExecutionRecord = {
                id: `${requestId}:${toolCallId}`,
                sessionId: sid,
                requestId,
                createdAt: Date.now(),
                toolCallId,
                toolName,
                arguments: {},
                status: rejected ? "rejected" : "success",
                summary: buildToolExecutionSummary(
                  toolName,
                  rejected ? "rejected" : "success",
                  content,
                ),
                detail: JSON.stringify(payload, null, 2),
                finishedAt: Date.now(),
              };
              return { ...prev, [sid]: [...current, created] };
            } else {
              const next = [...current];
              next[idx] = {
                ...next[idx],
                status: rejected ? "rejected" : "success",
                summary: buildToolExecutionSummary(
                  next[idx].toolName || toolName,
                  rejected ? "rejected" : "success",
                  content,
                ),
                detail: JSON.stringify(payload, null, 2),
                finishedAt: Date.now(),
              };
              return { ...prev, [sid]: next };
            }
          });
        }
      } else if (eventType === "approval_required") {
        const args = (payload.approval_args ?? {}) as { tool_calls?: ToolCallItem[] };
        const toolCalls = Array.isArray(args.tool_calls) ? args.tool_calls : [];
        if (toolCalls.length === 0) {
          return;
        } else {
          const idRaw = typeof payload.approval_id === "string" ? payload.approval_id : "";
          const approvalId = idRaw || `${requestId}-${Date.now()}`;
          const approvalTask: ApprovalTask = {
            id: approvalId,
            sessionId: sid,
            requestId,
            createdAt: Date.now(),
            payload: {
              message: typeof payload.content === "string" ? payload.content : "工具调用",
              description: typeof payload.description === "string" ? payload.description : "",
              args: { tool_calls: toolCalls },
            },
            handled: false,
          };
          setApprovalsBySession((prev) => {
            const current = prev[sid] ?? [];
            const exists = current.some((item) => item.id === approvalTask.id);
            return {
              ...prev,
              [sid]: exists
                ? current.map((item) => (item.id === approvalTask.id ? approvalTask : item))
                : [...current, approvalTask],
            };
          });
        }
      } else if (eventType === "usage") {
        const input = Number(payload.prompt_tokens ?? 0);
        const output = Number(payload.completion_tokens ?? 0);
        const total = Number(payload.total_tokens ?? input + output);
        setRuntimeBySession((prev) => ({
          ...prev,
          [sid]: {
            ...(prev[sid] ?? defaultRuntimeModel),
            usage: {
              inputTokens: Number.isFinite(input) ? input : (prev[sid] ?? defaultRuntimeModel).usage.inputTokens,
              outputTokens: Number.isFinite(output) ? output : (prev[sid] ?? defaultRuntimeModel).usage.outputTokens,
              totalTokens: Number.isFinite(total) ? total : (prev[sid] ?? defaultRuntimeModel).usage.totalTokens,
            },
          },
        }));
      } else if (eventType === "error") {
        streamTurnBySessionRef.current[sid] = (streamTurnBySessionRef.current[sid] ?? 0) + 1;
        const message = typeof payload.message === "string" ? payload.message : "运行异常";
        setLatestErrorBySession((prev) => ({ ...prev, [sid]: message }));
        setRuntimeBySession((prev) => ({
          ...prev,
          [sid]: { ...(prev[sid] ?? defaultRuntimeModel), status: "error", errorMessage: message },
        }));
        setSendingBySession((prev) => ({ ...prev, [sid]: false }));
      } else if (eventType === "done") {
        streamTurnBySessionRef.current[sid] = (streamTurnBySessionRef.current[sid] ?? 0) + 1;
        setRuntimeBySession((prev) => ({
          ...prev,
          [sid]: { ...(prev[sid] ?? defaultRuntimeModel), status: "done" },
        }));
        setSendingBySession((prev) => ({ ...prev, [sid]: false }));
        setSubmittingToolCallIdsBySession((prev) => ({ ...prev, [sid]: [] }));
      } else if (eventType === "subagent_started") {
        const subId = String(payload.subagent_id ?? "").trim();
        if (!subId) {
          return;
        } else {
          const thread: SubAgentThread = {
            id: subId,
            parentRequestId: requestId,
            sessionId: sid,
            createdAt: Date.now(),
            agentId: subId,
            title: typeof payload.title === "string" ? payload.title : subId,
            status: "running",
            chunks: [],
            startedAt: Date.now(),
          };
          setThreadsBySession((prev) => {
            const current = prev[sid] ?? [];
            return {
              ...prev,
              [sid]: current.some((item) => item.id === thread.id) ? current : [...current, thread],
            };
          });
          setActiveThreadBySession((prev) => ({ ...prev, [sid]: subId }));
        }
      } else if (eventType === "subagent_delta") {
        const subId = String(payload.subagent_id ?? "").trim();
        const delta = String(payload.content ?? "");
        if (!subId || !delta) {
          return;
        } else {
          setThreadsBySession((prev) => ({
            ...prev,
            [sid]: (prev[sid] ?? []).map((item) =>
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
          }));
        }
      } else if (eventType === "subagent_done" || eventType === "subagent_error") {
        const subId = String(payload.subagent_id ?? "").trim();
        if (!subId) {
          return;
        } else {
          setThreadsBySession((prev) => ({
            ...prev,
            [sid]: (prev[sid] ?? []).map((item) =>
              item.id === subId
                ? {
                    ...item,
                    status: eventType === "subagent_done" ? "success" : "error",
                    endedAt: Date.now(),
                    errorMessage: eventType === "subagent_error" ? String(payload.message ?? "") : item.errorMessage,
                  }
                : item,
            ),
          }));
        }
      } else if (eventType === "tool_call") {
        if (content) {
          appendMessageForSession(sid, createMessage(sid, "assistant", content, requestId));
        } else {
          return;
        }
      } else {
        // ignore unsupported event types
      }
    };

    const parseAndDispatch = (eventType: string, rawData: string) => {
      try {
        const parsed = JSON.parse(rawData) as {
          data?: unknown;
          seq?: unknown;
          session_id?: unknown;
          client_id?: unknown;
        };
        const seqNumber = Number(parsed.seq);
        const parsedClientId = String(parsed.client_id ?? "").trim();
        if (parsedClientId && parsedClientId !== clientId) {
          return;
        } else {
          // same client
        }
        if (Number.isFinite(seqNumber) && seqNumber >= 0) {
          const eventKey = `${clientId}:${seqNumber}`;
          if (seenEventSeqRef.current.has(eventKey)) {
            wbLog("sse:event:deduplicated", { eventType, seq: seqNumber });
            return;
          } else {
            seenEventSeqRef.current.add(eventKey);
          }
        } else {
          // missing request/seq
        }
        if (parsed && typeof parsed === "object") {
          onEvent(eventType, parsed as Record<string, unknown>);
        } else {
          return;
        }
      } catch (error) {
        wbLog("sse:parse:error", {
          eventType,
          rawData,
          error: String(error),
        });
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

    es.onopen = () => {
      setSseConnected(true);
    };

    es.onerror = () => {
      wbLog("sse:global:error");
      setSseConnected(false);
      setRuntimeBySession((prev) => {
        const next: Record<string, RuntimeState> = { ...prev };
        for (const sid of Object.keys(next)) {
          if (next[sid].status === "running") {
            next[sid] = { ...next[sid], status: "error", errorMessage: "SSE 连接异常" };
          } else {
            // keep current status
          }
        }
        return next;
      });
    };

    return () => {
      for (const type of eventTypes) {
        es.removeEventListener(type, (event) => {
          const msgEvent = event as MessageEvent;
          parseAndDispatch(type, msgEvent.data);
        });
      }
    };
  }, [clientId, defaultRuntimeModel]);

  const handleCreateSession = async () => {
    try {
      const result = await api.createSession({});
      ensureSessionSlot(result.session_id);
      setActiveSessionId(result.session_id);
      wbLog("session:create:success", result);
    } catch (error) {
      wbLog("session:create:error", { error: String(error) });
    }
  };

  const handleSendMessage = async (content: string) => {
    const sid = activeSessionId;
    if (!sid) {
      wbLog("sendMessage:skip-no-active-session");
      return;
    } else {
      // active session exists
    }
    wbLog("sendMessage:called", {
      rawContentLength: content.length,
      sessionId: sid,
      effectiveApiBaseUrl: resolvedApiBaseUrl || "http://127.0.0.1:8000",
    });
    const text = content.trim();
    if (!text) {
      wbLog("sendMessage:skip-empty");
      return;
    } else {
      setLatestErrorBySession((prev) => ({ ...prev, [sid]: undefined }));
      appendMessageForSession(sid, createMessage(sid, "user", text));
      setSendingBySession((prev) => ({ ...prev, [sid]: true }));
      setRuntimeBySession((prev) => ({
        ...prev,
        [sid]: { ...(prev[sid] ?? defaultRuntimeModel), status: "running", errorMessage: undefined },
      }));
      try {
        wbLog("sendMessage:request:start", {
          sessionId: sid,
          requestType: "message",
          textLength: text.length,
        });
        await api.submitMessage({
          session_id: sid,
          client_id: clientId,
          request_type: "message",
          content: text,
          source: "frontend",
        });
        wbLog("sendMessage:request:success");
      } catch (error) {
        const message = String(error);
        wbLog("sendMessage:request:error", { error: message });
        setSendingBySession((prev) => ({ ...prev, [sid]: false }));
        setLatestErrorBySession((prev) => ({ ...prev, [sid]: message }));
        setRuntimeBySession((prev) => ({
          ...prev,
          [sid]: { ...(prev[sid] ?? defaultRuntimeModel), status: "error", errorMessage: message },
        }));
      }
    }
  };

  const handleToolDecision = async (
    taskId: string,
    toolCallId: string,
    decision: ToolCallDecision,
  ) => {
    const sid = activeSessionId;
    if (!sid) {
      return;
    } else {
      // active session exists
    }
    wbLog("toolDecision:called", { taskId, toolCallId, decision, sessionId: sid });
    setSubmittingToolCallIdsBySession((prev) => {
      const current = prev[sid] ?? [];
      return { ...prev, [sid]: current.includes(toolCallId) ? current : [...current, toolCallId] };
    });
    setLatestErrorBySession((prev) => ({ ...prev, [sid]: undefined }));

    const task = (approvalsBySession[sid] ?? []).find((item) => item.id === taskId);
    if (!task) {
      wbLog("toolDecision:task-not-found", { taskId, toolCallId });
      setSubmittingToolCallIdsBySession((prev) => ({
        ...prev,
        [sid]: (prev[sid] ?? []).filter((id) => id !== toolCallId),
      }));
      return;
    } else {
      const approved = decision === "approve" ? [toolCallId] : [];
      const rejected = decision === "reject" ? [toolCallId] : [];
      const selectedToolCall = task.payload.args.tool_calls.find((item) => item.id === toolCallId);

      try {
        wbLog("toolDecision:resume:start", { taskId, toolCallId, approved, rejected });
        await api.submitResume(sid, {
          type: "selection",
          approved,
          rejected,
        }, "frontend", clientId);
        wbLog("toolDecision:resume:success");

        if (selectedToolCall) {
          upsertToolExecutionForSession(sid, {
            id: `${task.requestId}:${toolCallId}`,
            sessionId: sid,
            requestId: task.requestId,
            createdAt: Date.now(),
            toolCallId,
            toolName: selectedToolCall.name,
            arguments: selectedToolCall.arguments,
            status: decision === "approve" ? "running" : "rejected",
            summary: buildToolExecutionSummary(
              selectedToolCall.name,
              decision === "approve" ? "running" : "rejected",
            ),
            detail: decision === "approve" ? undefined : "该工具调用已被用户拒绝。",
            finishedAt: decision === "approve" ? undefined : Date.now(),
          });
        } else {
          // no matched tool_call entry in approval payload
        }

        setApprovalsBySession((prev) => ({
          ...prev,
          [sid]: (prev[sid] ?? [])
            .map((item) => {
              if (item.id !== taskId) {
                return item;
              } else {
                const remainedCalls = item.payload.args.tool_calls.filter((row) => row.id !== toolCallId);
                return {
                  ...item,
                  approvedIds: [...(item.approvedIds ?? []), ...approved],
                  rejectedIds: [...(item.rejectedIds ?? []), ...rejected],
                  handled: remainedCalls.length === 0,
                  decision: "selective" as const,
                  handledAt: Date.now(),
                  payload: {
                    ...item.payload,
                    args: {
                      ...item.payload.args,
                      tool_calls: remainedCalls,
                    },
                  },
                };
              }
            })
            .filter((item) => !item.handled),
        }));

        if (decision === "approve") {
          setRunningToolCallIdsBySession((prev) => {
            const current = prev[sid] ?? [];
            return { ...prev, [sid]: current.includes(toolCallId) ? current : [...current, toolCallId] };
          });
        } else {
          setRunningToolCallIdsBySession((prev) => ({
            ...prev,
            [sid]: (prev[sid] ?? []).filter((id) => id !== toolCallId),
          }));
        }

        setSubmittingToolCallIdsBySession((prev) => ({
          ...prev,
          [sid]: (prev[sid] ?? []).filter((id) => id !== toolCallId),
        }));
        setRuntimeBySession((prev) => ({
          ...prev,
          [sid]: { ...(prev[sid] ?? defaultRuntimeModel), status: "running" },
        }));
      } catch (error) {
        const message = String(error);
        wbLog("toolDecision:resume:error", { taskId, toolCallId, error: message });
        setSubmittingToolCallIdsBySession((prev) => ({
          ...prev,
          [sid]: (prev[sid] ?? []).filter((id) => id !== toolCallId),
        }));
        setLatestErrorBySession((prev) => ({ ...prev, [sid]: message }));
        setRuntimeBySession((prev) => ({
          ...prev,
          [sid]: { ...(prev[sid] ?? defaultRuntimeModel), status: "error", errorMessage: message },
        }));
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
      </header>

      <div className="app__body app__body--two-col">
        <main className="app__col">
          <MainChatPanel
            sessionId={activeSessionId}
            messages={activeMessages}
            approvals={activeApprovals}
            toolExecutions={activeToolExecutions}
            submittingToolCallIds={activeSubmittingToolCallIds}
            runningToolCallIds={activeRunningToolCallIds}
            completedToolCallIds={activeCompletedToolCallIds}
            sending={activeSending}
            onSendMessage={handleSendMessage}
            onDecideToolCall={handleToolDecision}
            disabled={!activeSessionId}
          />
        </main>

        <aside className="app__col app__col--aside">
          <RuntimeStatusPanel
            runtime={activeRuntime}
            latestError={activeLatestError}
            sseConnected={sseConnected}
          />
          <section className="panel thread-panel">
            <header className="panel__header">
              <div className="panel__title">
                team Agent
                <span className="pill">{activeThreads.length}</span>
              </div>
            </header>
            <div className="panel__body">
              <SubAgentThreadTabs
                threads={activeThreads}
                activeThreadId={activeThreadId}
                onSwitchThread={(threadId) =>
                  setActiveThreadBySession((prev) => ({ ...prev, [activeSessionId]: threadId }))
                }
              />
              <SubAgentThreadView thread={activeThread} />
            </div>
          </section>
          <section className="panel session-panel">
            <header className="panel__header session-panel__header">
              <div className="panel__title">
                历史会话
                <span className="pill">{sessionIds.length}</span>
              </div>
              <button
                type="button"
                className="session-panel__icon-btn"
                onClick={() => void handleCreateSession()}
                aria-label="新建会话"
                title="新建会话"
              >
                <IconPlus />
              </button>
            </header>
            <div className="panel__body session-panel__body">
              <div className="session-history-list">
                {sessionHistory.map((sid) => (
                  <div
                    key={sid}
                    className={`session-history-item ${sid === activeSessionId ? "session-history-item--active" : ""}`}
                  >
                    <button
                      type="button"
                      className="session-history-item__main"
                      onClick={() => setActiveSessionId(sid)}
                    >
                      {editingSessionId === sid ? (
                        <input
                          className="session-history-item__title-input"
                          value={editingTitleDraft}
                          onChange={(event) => setEditingTitleDraft(event.target.value)}
                          onClick={(event) => event.stopPropagation()}
                          onBlur={() => handleCommitEditSessionTitle(sid)}
                          onKeyDown={(event) => {
                            if (event.key === "Enter") {
                              event.preventDefault();
                              handleCommitEditSessionTitle(sid);
                            } else if (event.key === "Escape") {
                              event.preventDefault();
                              setEditingSessionId("");
                              setEditingTitleDraft("");
                            } else {
                              // keep editing
                            }
                          }}
                          autoFocus
                        />
                      ) : (
                        <span className="session-history-item__title">{getSessionTitle(sid)}</span>
                      )}
                    </button>
                    <button
                      type="button"
                      className="session-history-item__edit"
                      onClick={(event) => {
                        event.stopPropagation();
                        handleStartEditSessionTitle(sid);
                      }}
                      aria-label="编辑会话标题"
                      title="编辑会话标题"
                    >
                      <IconEdit />
                    </button>
                    {sid !== DEFAULT_SESSION_ID ? (
                      <button
                        type="button"
                        className="session-history-item__delete"
                        onClick={(event) => {
                          event.stopPropagation();
                          handleDeleteSession(sid);
                        }}
                        aria-label="删除会话"
                        title="删除会话"
                      >
                        <IconTrash />
                      </button>
                    ) : null}
                  </div>
                ))}
              </div>
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}
