import test from 'node:test'
import assert from 'node:assert/strict'
import { createTerminalOutputDecoder } from './terminal-output.js'

const encoded = text => Buffer.from(text, 'utf8').toString('base64')

test('filters CSI and complete OSC sequences terminated by BEL or ST', () => {
  const decoder = createTerminalOutputDecoder()
  assert.equal(decoder.push(encoded('before\u001b[31mred\u001b[0m\u001b]0;window title\u0007after')), 'beforeredafter')
  assert.equal(createTerminalOutputDecoder().push(encoded('left\u001b]2;title\u001b\\right')), 'leftright')
})

test('holds split OSC sequences until BEL or split ESC-backslash terminates them', () => {
  const belDecoder = createTerminalOutputDecoder()
  assert.equal(belDecoder.push(encoded('visible\u001b]0;private title')), 'visible')
  assert.equal(belDecoder.push(encoded(' continued')), '')
  assert.equal(belDecoder.push(encoded('\u0007visible again')), 'visible again')

  const stDecoder = createTerminalOutputDecoder()
  assert.equal(stDecoder.push(encoded('start\u001b]0;private')), 'start')
  assert.equal(stDecoder.push(encoded(' title\u001b')), '')
  assert.equal(stDecoder.push(encoded('\\end')), 'end')
})

test('preserves ordinary output and UTF-8 characters split between chunks', () => {
  const decoder = createTerminalOutputDecoder()
  const bytes = Buffer.from('你好\r\n')
  assert.equal(decoder.push(bytes.subarray(0, 2).toString('base64')), '')
  assert.equal(decoder.push(bytes.subarray(2).toString('base64')), '你好\r\n')
})
