import type {
  CancelTurnResult,
  MessageIn,
  SessionCreateIn,
  SessionCreateResult,
  SubmitResult,
} from "./types";

export interface ApiClientOptions {
  baseUrl?: string;
  fetchImpl?: typeof fetch;
}

export interface StreamEventEnvelope<T = unknown> {
  type: string;
  data: T;
}

function buildUrl(baseUrl: string, path: string): string {
  if (!baseUrl) {
    return path;
  } else {
    return `${baseUrl.replace(/\/+$/, "")}${path}`;
  }
}

async function parseJsonOrThrow<T>(response: Response): Promise<T> {
  if (response.ok) {
    return (await response.json()) as T;
  } else {
    let detail = `${response.status} ${response.statusText}`;
    try {
      const body = await response.json();
      if (typeof body?.detail === "string") {
        detail = body.detail;
      } else {
        detail = JSON.stringify(body);
      }
    } catch {
      // ignore non-json error body and use default detail
    }
    throw new Error(`API 请求失败: ${detail}`);
  }
}

export class DAgentsApiClient {
  private readonly baseUrl: string;

  private readonly fetchImpl: typeof fetch;

  constructor(options: ApiClientOptions = {}) {
    this.baseUrl = options.baseUrl ?? "";
    this.fetchImpl = options.fetchImpl ?? fetch;
  }

  async createSession(body: SessionCreateIn = {}): Promise<SessionCreateResult> {
    const response = await this.fetchImpl(buildUrl(this.baseUrl, "/v1/sessions"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    return parseJsonOrThrow<SessionCreateResult>(response);
  }

  async submitMessage(body: MessageIn): Promise<SubmitResult> {
    const response = await this.fetchImpl(buildUrl(this.baseUrl, "/v1/messages"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    return parseJsonOrThrow<SubmitResult>(response);
  }

  async submitResume(
    sessionId: string,
    resumeValue: unknown,
    source = "frontend",
  ): Promise<SubmitResult> {
    return this.submitMessage({
      session_id: sessionId,
      request_type: "resume",
      resume_value: resumeValue,
      source,
    });
  }

  async cancelCurrentTurn(sessionId: string): Promise<CancelTurnResult> {
    const sid = sessionId.trim();
    if (!sid) {
      throw new Error("sessionId 不能为空");
    } else {
      const response = await this.fetchImpl(
        buildUrl(this.baseUrl, `/v1/sessions/${encodeURIComponent(sid)}/cancel`),
        {
          method: "POST",
        },
      );
      return parseJsonOrThrow<CancelTurnResult>(response);
    }
  }

  streamUrl(requestId: string): string {
    return buildUrl(this.baseUrl, `/v1/streams/${encodeURIComponent(requestId)}`);
  }
}

