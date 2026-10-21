import test from 'node:test'
import assert from 'node:assert/strict'
import { highlightWorkspaceSource } from './workspace-syntax.js'

test('workspace code previews add language tokens and escape source markup', () => {
  const output = highlightWorkspaceSource('const label = "<script>"', 'javascript')
  assert.match(output, /hljs-keyword/)
  assert.match(output, /&lt;script&gt;/)
  assert.doesNotMatch(output, /<script>/)
})
