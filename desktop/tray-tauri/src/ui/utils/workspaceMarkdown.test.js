import { describe, expect, it } from 'vitest'
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
})
