import { afterEach, describe, expect, it, vi } from "vitest";
import {
  TerminalSession,
  buildTerminalWebSocketURL,
  decodeTerminalBytes,
  encodeBase64Bytes,
} from "./terminalSession.js";

class FakeWebSocket {
  static instances = [];
  static OPEN = 1;

  constructor(url) {
    this.url = url;
    this.readyState = 0;
    this.sent = [];
    FakeWebSocket.instances.push(this);
  }

  send(value) {
    this.sent.push(JSON.parse(value));
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  message(event) {
    this.onmessage?.({ data: JSON.stringify(event) });
  }

  disconnect() {
    this.readyState = 3;
    this.onclose?.();
  }

  close() {
    this.readyState = 3;
    this.onclose?.();
  }
}

afterEach(() => {
  FakeWebSocket.instances = [];
  vi.useRealTimers();
});

describe("TerminalSession", () => {
  it("builds a same-origin WebSocket URL and preserves byte payloads", () => {
    expect(buildTerminalWebSocketURL("agent/a", { protocol: "https:", host: "node.test:18765" })).toBe(
      "wss://node.test:18765/v1/agents/agent%2Fa/terminals/ws",
    );
    const encoded = encodeBase64Bytes("你好\r\n");
    expect(decodeTerminalBytes(encoded)).toBe("你好\r\n");
  });

  it("opens a terminal, sends input and resize, and decodes output", () => {
    const output = [];
    const statuses = [];
    const session = new TerminalSession(
      "agent-1",
      {
        onOutput: (event) => output.push(event),
        onStatus: (status) => statuses.push(status),
      },
      { WebSocket: FakeWebSocket, location: { protocol: "http:", host: "localhost:18765" } },
    );

    session.connect({ shell: "powershell", cwd: "C:\\work", rows: 30, cols: 100 });
    const socket = FakeWebSocket.instances[0];
    expect(socket.url).toBe("ws://localhost:18765/v1/agents/agent-1/terminals/ws");
    socket.open();
    expect(socket.sent[0]).toMatchObject({ type: "open", shell: "powershell", rows: 30, cols: 100 });

    socket.message({ type: "started", session_id: "session-1", terminal_id: "terminal-1" });
    expect(socket.sent.at(-1)).toEqual({ type: "resize", rows: 30, cols: 100 });
    expect(session.sendInput("echo 你好\r\n")).toBe(true);
    expect(socket.sent.at(-1)).toMatchObject({ type: "input" });
    expect(session.resize(40, 120)).toBe(true);
    expect(socket.sent.at(-1)).toEqual({ type: "resize", rows: 40, cols: 120 });

    socket.message({ type: "output", seq: 1, data: encodeBase64Bytes("ready\r\n") });
    expect(output[0]).toMatchObject({ text: "ready\r\n", seq: 1, replay: false });
    expect(statuses).toContain("connected");
  });

  it("resumes a detached session and surfaces replay gaps", () => {
    vi.useFakeTimers();
    const replayGaps = [];
    const session = new TerminalSession(
      "agent-2",
      { onReplayGap: (event) => replayGaps.push(event) },
      { WebSocket: FakeWebSocket, reconnectDelay: 50, location: { protocol: "http:", host: "node.test" } },
    );

    session.connect();
    const first = FakeWebSocket.instances[0];
    first.open();
    first.message({ type: "started", session_id: "session-2" });
    first.disconnect();
    vi.advanceTimersByTime(50);

    const resumed = FakeWebSocket.instances[1];
    resumed.open();
    expect(resumed.sent[0]).toEqual({ type: "resume", session_id: "session-2" });
    resumed.message({ type: "started", session_id: "session-2" });
    resumed.message({ type: "replay_gap", error: "terminal output exceeded replay buffer" });
    expect(replayGaps).toHaveLength(1);
    expect(session.status).toBe("connected");
  });

  it("does not reconnect after an explicit close", () => {
    vi.useFakeTimers();
    const session = new TerminalSession(
      "agent-3",
      {},
      { WebSocket: FakeWebSocket, reconnectDelay: 10, location: { protocol: "http:", host: "node.test" } },
    );
    session.connect();
    const socket = FakeWebSocket.instances[0];
    socket.open();
    session.close();
    vi.advanceTimersByTime(100);
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(session.status).toBe("closed");
  });

  it("does not reconnect after the server confirms termination", () => {
    vi.useFakeTimers();
    const exits = [];
    const session = new TerminalSession(
      "agent-terminated",
      { onExit: (event) => exits.push(event) },
      { WebSocket: FakeWebSocket, reconnectDelay: 10, location: { protocol: "http:", host: "node.test" } },
    );
    session.connect();
    const socket = FakeWebSocket.instances[0];
    socket.open();
    socket.message({ type: "started", session_id: "session-terminated" });
    socket.message({ type: "terminated", session_id: "session-terminated" });
    socket.disconnect();
    vi.advanceTimersByTime(100);

    expect(exits).toHaveLength(1);
    expect(session.status).toBe("exited");
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it("treats a protocol close as terminal and does not resume it", () => {
    vi.useFakeTimers();
    const session = new TerminalSession(
      "agent-closed",
      {},
      { WebSocket: FakeWebSocket, reconnectDelay: 10, location: { protocol: "http:", host: "node.test" } },
    );
    session.connect();
    const socket = FakeWebSocket.instances[0];
    socket.open();
    socket.message({ type: "started", session_id: "session-closed" });
    socket.message({ type: "closed", session_id: "session-closed" });
    socket.disconnect();
    vi.advanceTimersByTime(100);

    expect(session.status).toBe("closed");
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it("resumes an Agent-owned session and detaches without closing it", () => {
    const session = new TerminalSession(
      "agent-owned",
      {},
      { WebSocket: FakeWebSocket, location: { protocol: "http:", host: "node.test" } },
    );
    session.connect({ sessionId: "terminal-owned-1", rows: 20, cols: 90 });
    const socket = FakeWebSocket.instances[0];
    socket.open();
    expect(socket.sent[0]).toEqual({ type: "resume", session_id: "terminal-owned-1" });
    session.detach();
    expect(socket.sent).toHaveLength(1);
    expect(session.status).toBe("closed");
  });
});
