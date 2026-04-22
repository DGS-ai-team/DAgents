import type { ReactNode } from "react";

import type { RequestStatus, SubAgentStatus } from "../ui-contracts";

type StatusKind = "running" | "success" | "error" | "warn" | "idle";

const REQUEST_STATUS_MAP: Record<RequestStatus, { kind: StatusKind; label: string }> = {
  idle: { kind: "idle", label: "idle" },
  queued: { kind: "warn", label: "queued" },
  running: { kind: "running", label: "running" },
  done: { kind: "success", label: "done" },
  error: { kind: "error", label: "error" },
  cancelled: { kind: "idle", label: "cancelled" },
};

const SUB_AGENT_STATUS_MAP: Record<SubAgentStatus, { kind: StatusKind; label: string }> = {
  running: { kind: "running", label: "running" },
  success: { kind: "success", label: "success" },
  error: { kind: "error", label: "error" },
  timeout: { kind: "warn", label: "timeout" },
  cancelled: { kind: "idle", label: "cancelled" },
};

function StatusPillBase({ kind, children }: { kind: StatusKind; children: ReactNode }) {
  return (
    <span className={`pill pill--${kind}`}>
      <span className="pill__dot" />
      {children}
    </span>
  );
}

export function RequestStatusPill({ status }: { status: RequestStatus }) {
  const cfg = REQUEST_STATUS_MAP[status];
  return <StatusPillBase kind={cfg.kind}>{cfg.label}</StatusPillBase>;
}

export function SubAgentStatusPill({ status }: { status: SubAgentStatus }) {
  const cfg = SUB_AGENT_STATUS_MAP[status];
  return <StatusPillBase kind={cfg.kind}>{cfg.label}</StatusPillBase>;
}

export function RiskBadge({ level }: { level?: "low" | "medium" | "high" }) {
  if (!level) {
    return null;
  }
  const labels: Record<"low" | "medium" | "high", string> = {
    low: "低风险",
    medium: "中风险",
    high: "高风险",
  };
  return <span className={`badge badge--${level}`}>{labels[level]}</span>;
}

export function initialsOf(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) {
    return "?";
  }
  return trimmed.slice(0, 2).toUpperCase();
}

export function formatNumber(n: number): string {
  if (n >= 10000) {
    return `${(n / 1000).toFixed(1)}k`;
  }
  return n.toLocaleString();
}
