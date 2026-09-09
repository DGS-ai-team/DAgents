// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AgentEventSourcesPanel from './AgentEventSourcesPanel.vue'

const row = (id, owner='auto-a', revision=2) => ({ source_id:id, owner_agent_id:owner, root:`C:/data/${id}`, revision, enabled:true, hash_content:true, state:{last_error:''} })
const flush = () => new Promise(resolve => setTimeout(resolve, 0))
describe('AgentEventSourcesPanel', () => {
  afterEach(() => vi.restoreAllMocks())
  it('loads owner rows and creates with canonical JSON fields', async () => {
    const fetch = vi.fn().mockResolvedValueOnce({ok:true,json:async()=>[row('files')] }).mockResolvedValueOnce({ok:true,json:async()=>[]}); vi.stubGlobal('fetch', fetch)
    const w=mount(AgentEventSourcesPanel,{props:{agentId:'auto-a'}}); await flush(); await nextTick(); expect(w.text()).toContain('C:/data/files')
    await w.find('input[placeholder="例如：项目目录"]').setValue('docs'); await w.find('input[placeholder="绝对目录路径"]').setValue('C:/docs'); await w.find('form').trigger('submit');
    expect(fetch.mock.calls[1][0]).toBe('/v1/agents/auto-a/event-sources'); expect(JSON.parse(fetch.mock.calls[1][1].body)).toMatchObject({source_id:'docs',owner_agent_id:'auto-a',root:'C:/docs',hash_content:true})
  })
  it('toggles and deletes with incremented or expected revision', async () => {
    const fetch=vi.fn().mockResolvedValue({ok:true,json:async()=>[row('files')]}); vi.stubGlobal('fetch',fetch); const w=mount(AgentEventSourcesPanel,{props:{agentId:'auto-a'}}); await flush(); await nextTick(); const buttons=w.findAll('.source-actions button'); await buttons[0].trigger('click'); await flush(); await nextTick(); await buttons[1].trigger('click'); expect(fetch.mock.calls.some(c=>c[1]?.method==='POST'&&JSON.parse(c[1].body).revision===3)).toBe(true); expect(fetch.mock.calls.some(c=>c[0].includes('files?revision=2')&&c[1]?.method==='DELETE')).toBe(true)
  })
  it('reloads after a real 409 and agent switch without leaking old rows', async () => {
    const fetch=vi.fn().mockResolvedValueOnce({ok:true,json:async()=>[row('a')] }).mockResolvedValueOnce({ok:true,json:async()=>[row('b','auto-b')]}); vi.stubGlobal('fetch',fetch); const w=mount(AgentEventSourcesPanel,{props:{agentId:'auto-a'}}); await flush(); await nextTick(); expect(w.text()).toContain('C:/data/a'); await w.setProps({agentId:'auto-b'}); await flush(); await nextTick(); expect(w.text()).not.toContain('C:/data/a'); expect(w.text()).toContain('C:/data/b')
  })
  it('ignores a late response from the previous agent', async () => {
    let resolveA, resolveJson; const fetch=vi.fn().mockImplementation((url) => url.includes('auto-a') ? Promise.resolve({ok:true,json:()=>new Promise(r=>{resolveJson=r})}) : Promise.resolve({ok:true,json:async()=>[row('b','auto-b')]})); vi.stubGlobal('fetch',fetch)
    const w=mount(AgentEventSourcesPanel,{props:{agentId:'auto-a'}}); await w.setProps({agentId:'auto-b'}); await flush(); await nextTick(); expect(w.text()).toContain('C:/data/b'); resolveJson([row('a')]); await flush(); await nextTick(); expect(w.text()).not.toContain('C:/data/a')
  })
  it('shows a readable load error and disables source actions', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: false, text: async () => 'agent not found or not auto' })
    vi.stubGlobal('fetch', fetch)
    const w = mount(AgentEventSourcesPanel, { props: { agentId: 'normal-a' } })
    await flush(); await nextTick()
    expect(w.text()).toContain('找不到该 Agent')
    expect(w.find('form button').element.disabled).toBe(true)
  })
  it('shows a real 409 from toggle and reloads canonical state', async () => {
    const fetch=vi.fn().mockResolvedValueOnce({ok:true,json:async()=>[row('a')] }).mockResolvedValueOnce({ok:false,status:409,text:async()=>'{"error":"revision conflict"}' }).mockResolvedValueOnce({ok:true,json:async()=>[row('a','auto-a',3)]}); vi.stubGlobal('fetch',fetch)
    const w=mount(AgentEventSourcesPanel,{props:{agentId:'auto-a'}}); await flush(); await nextTick(); await w.findAll('button')[0].trigger('click'); await flush(); await nextTick(); expect(w.text()).toContain('配置版本冲突'); expect(fetch.mock.calls[1][1].method).toBe('POST'); expect(fetch.mock.calls[2][0]).toContain('/event-sources')
  })
  it('does not let a pending save response write after agent switch', async () => {
    let resolveSave; const fetch=vi.fn().mockImplementation((url,opts)=> opts?.method==='POST' ? new Promise(r=>{resolveSave=r}) : Promise.resolve({ok:true,json:async()=>[] })); vi.stubGlobal('fetch',fetch)
    const w=mount(AgentEventSourcesPanel,{props:{agentId:'auto-a'}}); await flush(); await w.find('input[placeholder="例如：项目目录"]').setValue('old'); await w.find('input[placeholder="绝对目录路径"]').setValue('C:/old'); await w.find('form').trigger('submit'); await w.setProps({agentId:'auto-b'}); resolveSave({ok:true,json:async()=>({})}); await flush(); await nextTick(); expect(w.find('input[placeholder="例如：项目目录"]').element.value).toBe(''); expect(w.text()).not.toContain('C:/old')
  })
  it('does not show deferred toggle errors after switching away and back', async () => {
    let rejectToggle
    const fetch = vi.fn().mockImplementation((url, opts) => {
      if (opts?.method === 'POST' && url.includes('auto-a')) return new Promise((_, reject) => { rejectToggle = reject })
      return Promise.resolve({ ok: true, json: async () => url.includes('auto-b') ? [row('b', 'auto-b')] : [row('a')] })
    })
    vi.stubGlobal('fetch', fetch)
    const w = mount(AgentEventSourcesPanel, { props: { agentId: 'auto-a' } })
    await flush(); await nextTick(); await w.findAll('button')[0].trigger('click')
    await w.setProps({ agentId: 'auto-b' }); await flush(); await nextTick()
    await w.setProps({ agentId: 'auto-a' }); await flush(); await nextTick()
    rejectToggle(new Error('旧 Agent 冲突')); await flush(); await nextTick()
    expect(w.text()).not.toContain('旧 Agent 冲突')
  })
  it('keeps the new agent mutation busy when an old A request resolves late', async () => {
    const saves = []
    const fetch = vi.fn().mockImplementation((url, opts) => {
      if (opts?.method === 'POST') return new Promise(resolve => saves.push(resolve))
      return Promise.resolve({ ok: true, json: async () => [] })
    })
    vi.stubGlobal('fetch', fetch)
    const w = mount(AgentEventSourcesPanel, { props: { agentId: 'auto-a' } })
    await flush(); await nextTick(); await w.find('form').trigger('submit')
    await w.setProps({ agentId: 'auto-b' }); await flush(); await nextTick()
    await w.setProps({ agentId: 'auto-a' }); await flush(); await nextTick(); await w.find('form').trigger('submit')
    expect(saves).toHaveLength(2); saves[0]({ ok: true, json: async () => ({}) }); await flush(); await nextTick()
    expect(w.find('form button').element.disabled).toBe(true)
    saves[1]({ ok: true, json: async () => ({}) }); await flush(); await nextTick()
  })
  it('ignores a duplicate save while the first request is busy', async () => {
    let resolveSave
    const fetch = vi.fn().mockImplementation((url, opts) => opts?.method === 'POST' ? new Promise(resolve => { resolveSave = resolve }) : Promise.resolve({ ok: true, json: async () => [] }))
    vi.stubGlobal('fetch', fetch)
    const w = mount(AgentEventSourcesPanel, { props: { agentId: 'auto-a' } })
    await flush(); await w.find('form').trigger('submit'); await w.find('form').trigger('submit')
    expect(fetch.mock.calls.filter(c => c[1]?.method === 'POST')).toHaveLength(1)
    resolveSave({ ok: true, json: async () => ({}) }); await flush(); await nextTick()
    expect(w.find('form button').element.disabled).toBe(false)
    await w.find('form').trigger('submit'); expect(fetch.mock.calls.filter(c => c[1]?.method === 'POST')).toHaveLength(2)
    resolveSave({ ok: true, json: async () => ({}) }); await flush()
  })
})
