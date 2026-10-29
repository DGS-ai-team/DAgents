import { describe, expect, it, vi } from 'vitest'
import { marked } from 'marked'
import { renderMarkdown } from './markdown.js'
import { renderWorkspaceMarkdown } from './workspaceMarkdown.js'

describe('workspace Markdown preview', () => {
  it('renders Markdown while removing remote and relative image requests', () => {
    const html = renderWorkspaceMarkdown('# Notes\n\n![remote](https://example.com/image.png) ![local](./chart.png) ![inline](data:image/png;base64,AAAA)')
    expect(html).toContain('<h1>Notes</h1>')
    expect(html).toContain('remote')
    expect(html).toContain('local')
    expect(html).not.toContain('https://example.com/image.png')
    expect(html).not.toContain('./chart.png')
    expect(html).toContain('data:image/png;base64,AAAA')
  })

  it('removes message-only code toolbar buttons from file previews', () => {
    const html = renderWorkspaceMarkdown('```js\nconst value = 1\n```')
    expect(html).not.toContain('data-markdown-action')
    expect(html).toContain('<span class="hljs-keyword">const</span> value = <span class="hljs-number">1</span>')
  })

  it('does not read or populate the chat Markdown cache while chat rendering still reuses it', () => {
    const source = `workspace-cache-isolation-${crypto.randomUUID()}`
    const parse = vi.spyOn(marked, 'parse')
    try {
      renderWorkspaceMarkdown(source)
      expect(parse).toHaveBeenCalledTimes(1)

      renderMarkdown(source)
      expect(parse).toHaveBeenCalledTimes(2)
      renderMarkdown(source)
      expect(parse).toHaveBeenCalledTimes(2)

      renderWorkspaceMarkdown(source)
      expect(parse).toHaveBeenCalledTimes(3)

      renderMarkdown(source)
      expect(parse).toHaveBeenCalledTimes(3)
    } finally {
      parse.mockRestore()
    }
  })

  it('keeps raw HTML inert through the shared sanitizer pipeline', () => {
    const html = renderWorkspaceMarkdown('<script>alert("xss")</script><img src=x onerror="alert(1)">')
    expect(html).not.toMatch(/<(?:script|img)\b/i)
    expect(html).not.toMatch(/<[^>]+\bonerror\s*=/i)
    expect(html).toContain('&lt;script&gt;')
    expect(html).toContain('&lt;img')
  })
})
