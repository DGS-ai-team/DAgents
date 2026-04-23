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

function debugLog(scope: string, message: string, payload?: unknown): void {
  const now = new Date().toISOString();
  if (payload === undefined) {
    console.log(`[DAgents:${scope}] ${now} ${message}`);
  } else {
    console.log(`[DAgents:${scope}] ${now} ${message}`, payload);
  }
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
    if (options.fetchImpl) {
      this.fetchImpl = options.fetchImpl;
    } else {
      // In browser, calling detached `fetch` can throw Illegal invocation.
      this.fetchImpl = globalThis.fetch.bind(globalThis);
    }
    debugLog("api-client", "initialized", {
      baseUrl: this.baseUrl,
      hasCustomFetch: Boolean(options.fetchImpl),
    });
  }

  private async postJson<T>(path: string, body: unknown): Promise<T> {
    const url = buildUrl(this.baseUrl, path);
    const startedAt = Date.now();
    debugLog("http", "request:start", {
      method: "POST",
      url,
      body,
    });
    try {
      const response = await this.fetchImpl(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const elapsedMs = Date.now() - startedAt;
      debugLog("http", "request:done", {
        method: "POST",
        url,
        status: response.status,
        ok: response.ok,
        elapsedMs,
      });
      return parseJsonOrThrow<T>(response);
    } catch (error) {
      const elapsedMs = Date.now() - startedAt;
      debugLog("http", "request:error", {
        method: "POST",
        url,
        elapsedMs,
        error: String(error),
      });
      throw error;
    }
  }

  async createSession(body: SessionCreateIn = {}): Promise<SessionCreateResult> {
    return this.postJson<SessionCreateResult>("/v1/sessions", body);
  }

  async submitMessage(body: MessageIn): Promise<SubmitResult> {
    return this.postJson<SubmitResult>("/v1/messages", body);
  }

  async submitResume(
    sessionId: string,
    resumeValue: unknown,
    source = "frontend",
    clientId = "default",
  ): Promise<SubmitResult> {
    return this.submitMessage({
      session_id: sessionId,
      client_id: clientId,
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
      debugLog("http", "request:done", {
        method: "POST",
        url: buildUrl(this.baseUrl, `/v1/sessions/${encodeURIComponent(sid)}/cancel`),
        status: response.status,
        ok: response.ok,
      });
      return parseJsonOrThrow<CancelTurnResult>(response);
    }
  }

  streamAllUrl(clientId: string): string {
    const query = `client_id=${encodeURIComponent(clientId)}`;
    const url = buildUrl(this.baseUrl, `/v1/streams?${query}`);
    debugLog("sse", "stream-all:url", { clientId, url });
    return url;
  }
}

