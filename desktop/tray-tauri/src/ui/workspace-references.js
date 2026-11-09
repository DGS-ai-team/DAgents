const ANCHOR_TYPES = new Set([
  'file',
  'text-range',
  'document-text',
  'sheet-range',
  'pdf-region',
  'image-region',
  'html-region',
])

const MAX_QUOTE_LENGTH = 2000

function cleanText(value, limit = MAX_QUOTE_LENGTH) {
  return String(value ?? '').replaceAll('\0', '').trim().slice(0, limit)
}

function cleanLocation(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value).flatMap(([key, item]) => {
    if (!/^[a-zA-Z][a-zA-Z0-9_]{0,31}$/.test(key)) return []
    if (typeof item === 'string') return [[key, cleanText(item, 300)]]
    if (typeof item === 'number' && Number.isFinite(item)) return [[key, item]]
    if (typeof item === 'boolean') return [[key, item]]
    return []
  }))
}

function normalizeWorkspacePath(value) {
  const path = String(value || '').replace(/^\.\//, '')
  const segments = path.split('/')
  if (!path || path.startsWith('/') || path.includes('\\') || path.includes(':')
    // Control characters can hide or confuse the actual resource path.
    // eslint-disable-next-line no-control-regex
    || /[\u0000-\u001f\u007f]/.test(path)
    || segments.some(part => !part || part === '.' || part === '..' || part.toLowerCase() === '.dagents')) {
    throw new Error('工作区文件路径无效，无法引用。')
  }
  return path
}

function fileReferenceKey(value) {
  const path = String(value || '').replaceAll('\\', '/')
  return /^[a-z]:\//i.test(path) || path.startsWith('//') ? path.toLowerCase() : path
}

export function normalizeWorkspaceReferenceAnchor(anchor = {}) {
  const type = ANCHOR_TYPES.has(anchor.type) ? anchor.type : 'file'
  return {
    type,
    label: cleanText(anchor.label, 160) || '整个文件',
    quote: cleanText(anchor.quote),
    location: cleanLocation(anchor.location),
  }
}

export function createWorkspaceReference({ id, path, name, fileRef, revision, draft = false, anchor, annotation = '', imageUrl: candidateImageUrl = '' } = {}) {
  const normalizedPath = normalizeWorkspacePath(path)
  const imageUrlValue = String(candidateImageUrl || '')
  const imageUrl = imageUrlValue.length <= 14 * 1024 * 1024
    && /^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/]+={0,2}$/.test(imageUrlValue)
    ? imageUrlValue
    : ''
  return {
    id: cleanText(id, 96) || `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    path: normalizedPath,
    name: cleanText(name, 255) || normalizedPath.split('/').at(-1),
    fileRef: cleanText(fileRef, 2048) || normalizedPath,
    revision: cleanText(revision, 200),
    draft: Boolean(draft),
    annotation: cleanText(annotation),
    imageUrl,
    anchor: normalizeWorkspaceReferenceAnchor(anchor),
  }
}

export function formatWorkspaceReferencesMessage({ text, references } = {}) {
  const items = (Array.isArray(references) ? references : []).map(reference => createWorkspaceReference(reference))
  const userText = String(text || '').trim()
  return userText || (items.length ? '请结合我引用的工作区内容处理。' : '')
}

export function composeWorkspaceMessage({ text, images = [], files = [], references = [] } = {}) {
  const normalizedReferences = (Array.isArray(references) ? references : []).map(reference => createWorkspaceReference(reference))
  const imageByUrl = new Map()
  for (const image of Array.isArray(images) ? images : []) {
    if (image?.url) imageByUrl.set(image.url, image)
  }
  const fileByPath = new Map()
  for (const file of Array.isArray(files) ? files : []) {
    if (file?.path) {
      fileByPath.set(fileReferenceKey(file.path), { path: file.path, name: file.name || '' })
    }
  }
  const fileReferences = [...fileByPath.values()]
  for (const reference of normalizedReferences) {
    if (reference.imageUrl && !imageByUrl.has(reference.imageUrl)) {
      imageByUrl.set(reference.imageUrl, { name: reference.name, url: reference.imageUrl })
    }
    const filePath = reference.fileRef || reference.path
    if (filePath) {
      const matchingKeys = new Set([fileReferenceKey(filePath), fileReferenceKey(reference.path)])
      for (let index = fileReferences.length - 1; index >= 0; index -= 1) {
        const file = fileReferences[index]
        if (!file.anchor && matchingKeys.has(fileReferenceKey(file.path))) fileReferences.splice(index, 1)
      }
      const structured = {
        id: reference.id,
        path: filePath,
        displayPath: reference.path,
        name: reference.name,
        revision: reference.revision || '',
        draft: Boolean(reference.draft),
        annotation: reference.annotation || '',
        anchor: reference.anchor,
      }
      if (!fileReferences.some(file => file.id === structured.id)) fileReferences.push(structured)
    }
  }
  return {
    text: formatWorkspaceReferencesMessage({ text, references: normalizedReferences }),
    images: [...imageByUrl.values()],
    files: fileReferences,
  }
}

export function hasWorkspaceMessageContent({ text = '', images = [], files = [], references = [] } = {}) {
  return Boolean(String(text || '').trim() || images.length || files.length || references.length)
}

export async function dispatchWorkspaceMessage({ agentId, text, images = [], files = [], references = [] } = {}, submit, onPrepared = () => {}) {
  const message = composeWorkspaceMessage({ text, images, files, references })
  const fileReferences = message.files.map(({ displayPath: _displayPath, ...file }) => ({
    ...file,
    path: String(file.path || ''),
    name: String(file.name || ''),
  }))
  const contentParts = message.images.map(image => ({ type: 'image_url', image_url: { url: image.url } }))
  onPrepared(message)
  await submit(agentId, message.text, contentParts, fileReferences)
  return message
}

export function workspaceReferencesForAgent(byAgent = {}, agentId = '') {
  return byAgent?.[agentId] || []
}

export function settleWorkspaceReferences(byAgent = {}, agentId = '', references = [], succeeded = false) {
  if (!succeeded || !agentId || !references.length) return byAgent
  const sentIds = new Set(references.map(reference => reference.id))
  return {
    ...byAgent,
    [agentId]: workspaceReferencesForAgent(byAgent, agentId).filter(reference => !sentIds.has(reference.id)),
  }
}

export function textRangeAnchor(source, start, end, label = '文本选区') {
  const text = String(source || '')
  const from = Math.max(0, Math.min(text.length, Number(start) || 0))
  const to = Math.max(from, Math.min(text.length, Number(end) || 0))
  const before = text.slice(0, from)
  const selected = text.slice(from, to)
  const startLine = before.split('\n').length
  const startColumn = from - (before.lastIndexOf('\n') + 1) + 1
  const endBefore = text.slice(0, to)
  const endLine = endBefore.split('\n').length
  const endColumn = to - (endBefore.lastIndexOf('\n') + 1) + 1
  return normalizeWorkspaceReferenceAnchor({
    type: selected ? 'text-range' : 'file',
    label: selected ? `${label} · 第 ${startLine}–${endLine} 行` : '整个文件',
    quote: selected,
    location: selected ? { start, end, startLine, startColumn, endLine, endColumn } : {},
  })
}
