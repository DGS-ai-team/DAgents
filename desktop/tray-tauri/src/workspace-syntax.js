import hljs from 'highlight.js/lib/common'

export function highlightWorkspaceSource(source, language = '') {
  const value = String(source || '')
  if (language && hljs.getLanguage(language)) return hljs.highlight(value, { language }).value
  return hljs.highlightAuto(value).value
}
