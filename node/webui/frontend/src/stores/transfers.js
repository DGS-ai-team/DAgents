import { reactive } from "vue";
import * as api from "../api/node.js";

const RECONNECT_MS = 5000;

export const transferStore = reactive({
  items: [],
  maxConcurrentFiles: 0,
  connected: false,
  error: "",
  lastSeq: 0,
});

let eventSource = null;
let reconnectTimer = null;
let stopped = false;

function normalize(item) {
  if (!item || typeof item !== "object") return null;
  const id = String(item.transfer_id || "").trim();
  if (!id) return null;
  return {
    ...item,
    transfer_id: id,
    status: String(item.status || "queued"),
    progress: Number.isFinite(Number(item.progress)) ? Number(item.progress) : 0,
    bytes_done: Number(item.bytes_done) || 0,
    total_bytes: Number(item.total_bytes) || 0,
    updated_at: item.updated_at || new Date().toISOString(),
  };
}

export function replaceTransfers(payload) {
  const items = Array.isArray(payload?.transfers)
    ? payload.transfers.map(normalize).filter(Boolean)
    : [];
  transferStore.items = items;
  const max = Number(payload?.max_concurrent_files);
  transferStore.maxConcurrentFiles = Number.isFinite(max) && max > 0 ? max : 0;
  transferStore.error = "";
}

export function applyTransferEvent(data) {
  const next = normalize(data);
  if (!next) return;
  const index = transferStore.items.findIndex((item) => item.transfer_id === next.transfer_id);
  if (index < 0) {
    transferStore.items.unshift(next);
  } else {
    transferStore.items[index] = { ...transferStore.items[index], ...next };
  }
}

export async function refreshTransfers() {
  try {
    replaceTransfers(await api.listTransfers());
  } catch (error) {
    transferStore.error = error?.message || String(error);
  }
}

function eventURL() {
  const params = new URLSearchParams();
  if (transferStore.lastSeq > 0) params.set("after_seq", String(transferStore.lastSeq));
  else params.set("live", "1");
  return `/v1/transfers/events?${params}`;
}

export function startTransferEvents() {
  stopTransferEvents();
  stopped = false;
  const open = () => {
    if (stopped) return;
    eventSource = new EventSource(eventURL());
    eventSource.onopen = () => {
      transferStore.connected = true;
      transferStore.error = "";
    };
    eventSource.onerror = () => {
      transferStore.connected = false;
      eventSource?.close();
      eventSource = null;
      if (!stopped) reconnectTimer = setTimeout(open, RECONNECT_MS);
    };
    eventSource.addEventListener("transfer.updated", (event) => {
      try {
        const envelope = JSON.parse(event.data || "{}");
        transferStore.lastSeq = Number(event.lastEventId || envelope.seq || transferStore.lastSeq) || transferStore.lastSeq;
        applyTransferEvent(envelope.data && typeof envelope.data === "object" ? envelope.data : envelope);
      } catch {
        /* ignore malformed progress events; hydration repairs the snapshot */
      }
    });
  };
  open();
}

export function stopTransferEvents() {
  stopped = true;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  eventSource?.close();
  eventSource = null;
  transferStore.connected = false;
}

export async function cancelTransfer(transferId) {
  const id = String(transferId || "").trim();
  if (!id) return;
  try {
    await api.cancelTransfer(id);
  } catch (error) {
    transferStore.error = error?.message || String(error);
    throw error;
  }
}
