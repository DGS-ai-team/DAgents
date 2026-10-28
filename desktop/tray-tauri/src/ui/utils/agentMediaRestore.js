async function restoreMediaItems(items, loadSource) {
  if (!Array.isArray(items)) return items
  return Promise.all(items.map(async item => {
    if (!item || typeof item !== "object" || !item.url) return item
    return { ...item, url: await loadSource(item.url) }
  }))
}

/** Resolve Node media URLs in both user entries and nested tool-result data. */
export async function restoreTranscriptMedia(snapshot, loadSource) {
  const transcript = Array.isArray(snapshot?.transcript) ? snapshot.transcript : []
  const rows = await Promise.all(transcript.map(async entry => {
    const out = { ...entry }
    if (Array.isArray(entry.images)) out.images = await Promise.all(entry.images.map(loadSource))
    if (Array.isArray(entry.media)) out.media = await restoreMediaItems(entry.media, loadSource)
    if (entry.data && typeof entry.data === "object" && !Array.isArray(entry.data)) {
      const data = { ...entry.data }
      if (Array.isArray(entry.data.media)) data.media = await restoreMediaItems(entry.data.media, loadSource)
      out.data = data
    }
    return out
  }))
  return { ...snapshot, transcript: rows }
}
