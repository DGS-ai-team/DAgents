async function restoreMediaItems(items, loadSource) {
  if (!Array.isArray(items)) return items
  let changed = false
  const restored = await Promise.all(items.map(async item => {
    if (!item || typeof item !== "object" || !item.url) return item
    const url = await loadSource(item.url)
    if (url === item.url) return item
    changed = true
    return { ...item, url }
  }))
  return changed ? restored : items
}

function needsSourceRestore(source) {
  const raw = String(source || "")
  return Boolean(raw && !raw.startsWith("data:") && !raw.startsWith("blob:"))
}

function hasMediaSources(entry) {
  if (Array.isArray(entry?.images) && entry.images.some(needsSourceRestore)) return true
  const hasItemUrl = items => Array.isArray(items) && items.some(item => item && typeof item === "object" && needsSourceRestore(item.url))
  return hasItemUrl(entry?.media) || hasItemUrl(entry?.data?.media)
}

async function restoreEntryMedia(entry, loadSource) {
  let out = entry
  const set = (key, value) => {
    if (out === entry) out = { ...entry }
    out[key] = value
  }
  if (Array.isArray(entry.images)) {
    const images = await Promise.all(entry.images.map(loadSource))
    if (images.some((image, index) => image !== entry.images[index])) set("images", images)
  }
  if (Array.isArray(entry.media)) {
    const media = await restoreMediaItems(entry.media, loadSource)
    if (media !== entry.media) set("media", media)
  }
  if (entry.data && typeof entry.data === "object" && !Array.isArray(entry.data) && Array.isArray(entry.data.media)) {
    const media = await restoreMediaItems(entry.data.media, loadSource)
    if (media !== entry.data.media) set("data", { ...entry.data, media })
  }
  return out
}

/** Resolve Node media URLs in both user entries and nested tool-result data. */
export async function restoreTranscriptMedia(snapshot, loadSource) {
  const transcript = Array.isArray(snapshot?.transcript) ? snapshot.transcript : []
  const rows = transcript.slice()
  const pending = []
  let changed = false
  transcript.forEach((entry, index) => {
    if (!entry || typeof entry !== "object" || !hasMediaSources(entry)) return
    pending.push(restoreEntryMedia(entry, loadSource).then(restored => {
      if (restored === entry) return
      rows[index] = restored
      changed = true
    }))
  })
  if (pending.length === 0) return snapshot
  await Promise.all(pending)
  return changed ? { ...snapshot, transcript: rows } : snapshot
}

function sameMediaUrls(left, right) {
  if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) return false
  return left.every((item, index) => {
    const source = item && typeof item === "object" ? item.url : item
    const target = right[index] && typeof right[index] === "object" ? right[index].url : right[index]
    return source === target
  })
}

function mergeMediaFields(current, restored) {
  let next = current
  const set = (key, value) => {
    if (next === current) next = { ...current }
    next[key] = value
  }
  if (Array.isArray(restored.images) && !sameMediaUrls(current.images, restored.images)) {
    set("images", restored.images)
  }
  if (Array.isArray(restored.media) && !sameMediaUrls(current.media, restored.media)) {
    set("media", restored.media)
  }
  if (Array.isArray(restored.data?.media) && !sameMediaUrls(current.data?.media, restored.data.media)) {
    set("data", { ...current.data, media: restored.data.media })
  }
  return next
}

/** Rebase restored media onto the latest transcript, preserving unrelated patches. */
export function mergeRestoredTranscriptMedia(latestTranscript, restoredEntries) {
  if (!Array.isArray(latestTranscript) || !Array.isArray(restoredEntries)) return null
  const restoredById = new Map(restoredEntries
    .map(entry => [String(entry?.item_id || entry?.id || "").trim(), entry])
    .filter(([itemId]) => itemId))
  let changed = false
  const transcript = latestTranscript.map(current => {
    const itemId = String(current?.item_id || current?.id || "").trim()
    const restored = itemId ? restoredById.get(itemId) : null
    if (!restored || String(current?.item_version ?? "") !== String(restored?.item_version ?? "")) return current
    const merged = mergeMediaFields(current, restored)
    if (merged !== current) changed = true
    return merged
  })
  return changed ? transcript : null
}
