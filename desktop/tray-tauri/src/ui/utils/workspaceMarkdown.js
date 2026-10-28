import { renderMarkdown } from './markdown.js'

// Workspace documents are previews, so they must not fetch image resources
// from the network simply because a file was opened. Inline data images stay
// available; all other image references degrade to their accessible alt text.
export function renderWorkspaceMarkdown(source) {
  const html = renderMarkdown(source)
  if (typeof DOMParser === 'undefined') {
    return html.replace(/<img\b([^>]*)>/gi, (_tag, attributes) => {
      const alt = /\balt="([^"]*)"/i.exec(attributes)?.[1] || ''
      const src = /\bsrc="([^"]*)"/i.exec(attributes)?.[1] || ''
      return /^data:image\//i.test(src) ? `<img alt="${alt}" src="${src}">` : alt
    }).replace(/<div class="markdown-code-block__toolbar">[\s\S]*?<\/div>/g, '')
  }
  const document = new DOMParser().parseFromString(html, 'text/html')
  for (const image of document.querySelectorAll('img')) {
    const sourceUrl = image.getAttribute('src') || ''
    if (!/^data:image\//i.test(sourceUrl)) image.replaceWith(document.createTextNode(image.getAttribute('alt') || ''))
  }
  document.querySelectorAll('.markdown-code-block__toolbar').forEach(toolbar => toolbar.remove())
  return document.body.innerHTML
}
