import test from 'node:test'
import assert from 'node:assert/strict'
import { isCurrentWorkgroupRequest, nextWorkgroupRequestState } from './workgroup-state.js'

test('switching groups invalidates requests and polling from the previous group', () => {
  const first = nextWorkgroupRequestState({ epoch: 0 }, 'wg-a')
  const request = { epoch: first.epoch, workgroupId: first.workgroupId }
  const second = nextWorkgroupRequestState(first, 'wg-b')
  assert.equal(isCurrentWorkgroupRequest(request, first), true)
  assert.equal(isCurrentWorkgroupRequest(request, second), false)
})

test('unmount invalidates the active group request', () => {
  const state = nextWorkgroupRequestState(null, 'wg-a')
  assert.equal(isCurrentWorkgroupRequest({ epoch: state.epoch, workgroupId: 'wg-a' }, { ...state, mounted: false }), false)
})
