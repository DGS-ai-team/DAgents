import { reactive } from "vue";

const WORKGROUP_READ_KEY = "dagents_webui_workgroup_read_seq";

function toSeq(value) {
  const seq = Number(value);
  return Number.isFinite(seq) && seq > 0 ? Math.floor(seq) : 0;
}

function loadReadSeq() {
  try {
    const raw = localStorage.getItem(WORKGROUP_READ_KEY);
    const parsed = raw ? JSON.parse(raw) : {};
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    return Object.fromEntries(
      Object.entries(parsed)
        .map(([id, seq]) => [String(id).trim(), toSeq(seq)])
        .filter(([id, seq]) => id && seq > 0),
    );
  } catch {
    return {};
  }
}

const unreadStore = reactive({
  workgroupLatestSeq: {},
  workgroupReadSeq: loadReadSeq(),
});

function workgroupKey(workgroupId) {
  return String(workgroupId || "").trim();
}

function persistReadSeq() {
  try {
    localStorage.setItem(WORKGROUP_READ_KEY, JSON.stringify(unreadStore.workgroupReadSeq));
  } catch {
    // Storage may be unavailable in private browsing or embedded webviews.
  }
}

export function noteWorkgroupTimeline(workgroupId, seq) {
  const id = workgroupKey(workgroupId);
  const latest = toSeq(seq);
  if (!id || latest <= (unreadStore.workgroupLatestSeq[id] || 0)) return;
  unreadStore.workgroupLatestSeq[id] = latest;
}

export function markWorkgroupRead(workgroupId, seq) {
  const id = workgroupKey(workgroupId);
  if (!id) return;
  const next = toSeq(seq ?? unreadStore.workgroupLatestSeq[id]);
  const current = unreadStore.workgroupReadSeq[id] || 0;
  if (next <= current) return;
  unreadStore.workgroupReadSeq[id] = next;
  persistReadSeq();
}

export function hasWorkgroupUnread(workgroupId) {
  const id = workgroupKey(workgroupId);
  if (!id) return false;
  return (unreadStore.workgroupLatestSeq[id] || 0) > (unreadStore.workgroupReadSeq[id] || 0);
}

export function resetUnreadStoreForTests() {
  unreadStore.workgroupLatestSeq = {};
  unreadStore.workgroupReadSeq = {};
}

export { unreadStore };
