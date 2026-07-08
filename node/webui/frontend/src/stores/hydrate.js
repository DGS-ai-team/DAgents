import * as api from "../api/node.js";
import { reportDesktopUIFocus } from "../api/desktop.js";
import { chromeStore } from "./chrome.js";
import { clearHitl, enqueueHitlRequired, hitlStore } from "./hitl.js";
import { setChildAwaitingApproval } from "./remoteWorkers.js";
import {
  applyHydrateSeqHint,
  applyHydrateTurnState,
  ackSessionAfterHydrate,
  ensureSession,
  finishTurn,
  sessionStore,
} from "./session.js";
import { loadTranscriptFromHydrate } from "./transcript.js";

/** ensureSession → GET /hydrate → 灌 transcript + pending HITL + SSE 水位（F-H7/H8/H9/H17, F-X6）。 */
export async function hydrateSession() {
  const sessionId = await ensureSession();
  const data = await api.getSessionHydrate(sessionId);
  loadTranscriptFromHydrate(data?.transcript);
  clearHitl();
  const { approval } = enqueueHitlRequired(data?.pending_hitl);
  if (approval?.child_session_id) {
    setChildAwaitingApproval(approval.child_session_id, true);
  }
  applyHydrateSeqHint(data?.sse_seq_hint);
  ackSessionAfterHydrate();
  applyHydrateTurnState({
    run_turn_phase: data?.run_turn_phase,
    has_active_turn: !!data?.has_active_turn,
    pending_hitl: data?.pending_hitl,
  });
  if (data?.pending_hitl?.items?.length) {
    finishTurn();
  }
  chromeStore.hitlQueueLen = hitlStore.queue.length;
  void reportDesktopUIFocus(sessionId);
  return data;
}

/** F-H10：浏览器 F5 / bfcache 恢复后重新 hydrate（与 F-H7 同路径）。 */
export async function refreshSessionAfterPageRestore() {
  return hydrateSession();
}

/** 解析 Shell 深链 ?session=（F-U3）。 */
export function consumeStartupURL() {
  if (typeof window === "undefined") return;
  const params = new URLSearchParams(window.location.search);
  const session = params.get("session")?.trim();
  if (session) {
    sessionStore.sessionId = session;
    try {
      localStorage.setItem("dagents_webui_session_id", session);
    } catch {
      /* ignore */
    }
  }
}
