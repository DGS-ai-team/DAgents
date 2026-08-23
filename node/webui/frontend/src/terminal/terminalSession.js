const DEFAULT_NODE_ORIGIN = "ws://127.0.0.1:18765";
const SOCKET_OPEN = 1;

function currentLocation() {
  return typeof globalThis.location === "object" && globalThis.location ? globalThis.location : null;
}

export function buildTerminalWebSocketURL(agentId, locationLike = currentLocation()) {
  const id = encodeURIComponent(String(agentId || "").trim());
  if (!id) throw new Error("agent_id is required");

  if (!locationLike?.host) {
    return `${DEFAULT_NODE_ORIGIN}/v1/agents/${id}/terminals/ws`;
  }
  const protocol = locationLike.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${locationLike.host}/v1/agents/${id}/terminals/ws`;
}

function byteArray(value) {
  if (value instanceof Uint8Array) return value;
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  return new TextEncoder().encode(String(value ?? ""));
}

export function encodeBase64Bytes(value) {
  const bytes = byteArray(value);
  let binary = "";
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
  }
  return typeof globalThis.btoa === "function" ? globalThis.btoa(binary) : Buffer.from(bytes).toString("base64");
}

export function decodeBase64Bytes(value) {
  const encoded = String(value || "");
  if (!encoded) return new Uint8Array();
  const binary = typeof globalThis.atob === "function" ? globalThis.atob(encoded) : Buffer.from(encoded, "base64").toString("binary");
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes;
}

export function decodeTerminalBytes(value) {
  const bytes = value instanceof Uint8Array ? value : decodeBase64Bytes(value);
  if (typeof TextDecoder === "function") return new TextDecoder().decode(bytes);
  let text = "";
  for (const byte of bytes) text += String.fromCharCode(byte);
  return text;
}

function isOpen(socket) {
  return Boolean(socket && socket.readyState === SOCKET_OPEN);
}

function safeCallback(callback, ...args) {
  if (typeof callback !== "function") return;
  try {
    callback(...args);
  } catch {
    // UI callbacks must not break the transport's reconnect loop.
  }
}

/**
 * Small browser-side client for the agent terminal WebSocket protocol.
 * It intentionally keeps terminal rendering separate so a future xterm
 * integration can consume the same reconnect/replay semantics.
 */
export class TerminalSession {
  constructor(agentId, callbacks = {}, options = {}) {
    this.agentId = String(agentId || "").trim();
    this.callbacks = callbacks || {};
    this.WebSocketCtor = options.WebSocket || globalThis.WebSocket;
    this.location = options.location;
    this.reconnectDelay = Number(options.reconnectDelay || 500);
    this.maxReconnectDelay = Number(options.maxReconnectDelay || 5000);
    this.ws = null;
    this.sessionId = "";
    this.terminalId = "";
    this.config = {};
    this.rows = 24;
    this.cols = 80;
    this.reconnectTimer = null;
    this.reconnectAttempt = 0;
    this.shouldReconnect = false;
    this.closedByUser = false;
    this.processExited = false;
    this.status = "idle";
  }

  connect(config = {}) {
    if (!this.agentId) return this._fail(new Error("agent_id is required"));
    if (!this.WebSocketCtor) return this._fail(new Error("WebSocket is unavailable"));
    this.config = { ...config };
    if (Object.prototype.hasOwnProperty.call(config, "sessionId")) {
      this.sessionId = String(config.sessionId || "").trim();
      this.terminalId = this.sessionId;
    }
    this.rows = Number(config.rows || this.rows);
    this.cols = Number(config.cols || this.cols);
    this.shouldReconnect = true;
    this.closedByUser = false;
    this.processExited = false;
    this.reconnectAttempt = 0;
    if (isOpen(this.ws) || this.ws?.readyState === 0) return;
    this._openSocket();
  }

  reconnectNow() {
    if (!this.shouldReconnect || this.closedByUser || this.processExited) return;
    if (isOpen(this.ws) || this.ws?.readyState === 0) return;
    this._clearReconnectTimer();
    this._openSocket();
  }

  sendInput(value) {
    return this._send({ type: "input", data: encodeBase64Bytes(value) });
  }

  resize(rows, cols) {
    this.rows = Math.max(1, Number(rows) || 24);
    this.cols = Math.max(1, Number(cols) || 80);
    return this._send({ type: "resize", rows: this.rows, cols: this.cols });
  }

  terminate() {
    const sent = this._send({ type: "terminate" });
    if (sent) {
      // An explicit user termination is authoritative for reconnect policy.
      // The server may need a few seconds to force-close the PTY, but the
      // browser must not resume a session that is being removed.
      this.shouldReconnect = false;
      this._clearReconnectTimer();
      this._setStatus("terminating");
    }
    return sent;
  }

  close() {
    this.shouldReconnect = false;
    this.closedByUser = true;
    this._clearReconnectTimer();
    const socket = this.ws;
    if (isOpen(socket)) this._send({ type: "close" });
    this.ws = null;
    try {
      socket?.close();
    } catch {
      // The socket may already be closing.
    }
    this._setStatus("closed");
  }

  // Detach the browser transport without sending the protocol-level close
  // command. Agent-owned sessions continue running and can be resumed later.
  detach() {
    this.shouldReconnect = false;
    this.closedByUser = true;
    this._clearReconnectTimer();
    const socket = this.ws;
    this.ws = null;
    try {
      socket?.close();
    } catch {
      // The socket may already be closing.
    }
    this._setStatus("closed");
  }

  _openSocket() {
    this._clearReconnectTimer();
    let socket;
    try {
      socket = new this.WebSocketCtor(buildTerminalWebSocketURL(this.agentId, this.location));
    } catch (error) {
      this._fail(error);
      this._scheduleReconnect();
      return;
    }
    this.ws = socket;
    this._setStatus(this.sessionId ? "reconnecting" : "connecting");
    socket.onopen = () => {
      this.reconnectAttempt = 0;
      const command = this.sessionId
        ? { type: "resume", session_id: this.sessionId }
        : {
            type: "open",
            target_kind: this.config.targetKind || "local",
            target_id: this.config.targetId || undefined,
            shell: this.config.shell || undefined,
            cwd: this.config.cwd || undefined,
            rows: this.rows,
            cols: this.cols,
          };
      this._send(command);
    };
    socket.onmessage = (message) => this._handleMessage(message?.data);
    socket.onerror = () => safeCallback(this.callbacks.onError, new Error("terminal websocket error"));
    socket.onclose = () => {
      if (this.ws === socket) this.ws = null;
      if (this.closedByUser || !this.shouldReconnect || this.processExited) return;
      this._setStatus("disconnected");
      this._scheduleReconnect();
    };
  }

  _handleMessage(raw) {
    if (typeof raw !== "string") {
      if (raw instanceof ArrayBuffer) raw = new TextDecoder().decode(new Uint8Array(raw));
      else return;
    }
    let event;
    try {
      event = JSON.parse(raw);
    } catch (error) {
      safeCallback(this.callbacks.onError, new Error(`invalid terminal event: ${error.message}`));
      return;
    }
    safeCallback(this.callbacks.onEvent, event);
    switch (event.type) {
      case "started":
        this.sessionId = String(event.session_id || this.sessionId || "");
        this.terminalId = String(event.terminal_id || this.terminalId || "");
        this._setStatus("connected");
        if (this.rows && this.cols) this._send({ type: "resize", rows: this.rows, cols: this.cols });
        break;
      case "output": {
        const bytes = decodeBase64Bytes(event.data);
        safeCallback(this.callbacks.onOutput, {
          text: decodeTerminalBytes(bytes),
          bytes,
          seq: Number(event.seq || 0),
          replay: Boolean(event.replay),
          event,
        });
        break;
      }
      case "replay_gap":
        safeCallback(this.callbacks.onReplayGap, event);
        break;
      case "exited":
        this.processExited = true;
        this.shouldReconnect = false;
        this._setStatus("exited");
        safeCallback(this.callbacks.onExit, event);
        break;
      case "terminated":
        // The server removes a session after an explicit terminate command and
        // closes the socket. Treat that lifecycle event as terminal rather than
        // allowing the reconnect loop to resume a session that no longer exists.
        this.processExited = true;
        this.shouldReconnect = false;
        this._setStatus("exited");
        safeCallback(this.callbacks.onExit, event);
        break;
      case "closed":
        // A protocol-level close is also authoritative. This is distinct from
        // a transport disconnect: the session must not be resumed implicitly.
        this.processExited = true;
        this.shouldReconnect = false;
        this._setStatus("closed");
        safeCallback(this.callbacks.onExit, event);
        break;
      case "error":
        safeCallback(this.callbacks.onError, new Error(String(event.error || "terminal error")), event);
        break;
      default:
        break;
    }
  }

  _send(command) {
    if (!isOpen(this.ws)) return false;
    try {
      this.ws.send(JSON.stringify(command));
      return true;
    } catch (error) {
      safeCallback(this.callbacks.onError, error);
      return false;
    }
  }

  _scheduleReconnect() {
    if (this.reconnectTimer || !this.shouldReconnect || this.closedByUser || this.processExited) return;
    const delay = Math.min(this.maxReconnectDelay, this.reconnectDelay * 2 ** this.reconnectAttempt);
    this.reconnectAttempt += 1;
    this._setStatus("reconnecting");
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.shouldReconnect && !this.closedByUser && !this.processExited) this._openSocket();
    }, delay);
  }

  _clearReconnectTimer() {
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
  }

  _setStatus(status) {
    this.status = status;
    safeCallback(this.callbacks.onStatus, status);
  }

  _fail(error) {
    this._setStatus("error");
    safeCallback(this.callbacks.onError, error);
    return false;
  }
}

export default TerminalSession;
