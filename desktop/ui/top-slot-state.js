const PAGE_ICONS = {
  chat: 'panel',
  settings: 'settings',
  terminal: 'terminal',
  file: 'folder-open',
  browser: 'browser',
  workgroups: 'workgroup',
}

const PAGE_LABELS = {
  chat: '对话',
  settings: '设置',
  terminal: '终端',
  file: '工作区',
  browser: '浏览器',
  workgroups: '工作组',
  'auto-overview': '自主总览',
}

export function topSlotState({ tab = 'chat', collapsed = false, autoOverview = false } = {}) {
  const page = autoOverview ? 'auto-overview' : (PAGE_ICONS[tab] ? tab : 'chat')
  const pageLabel = PAGE_LABELS[page]
  const interactive = page !== 'settings' && page !== 'auto-overview'
  return {
    icon: page === 'auto-overview' ? 'auto' : (PAGE_ICONS[page] || 'panel'),
    page,
    pageLabel,
    interactive,
    actionLabel: interactive ? `${pageLabel} · ${collapsed ? '展开' : '收起'}侧栏` : pageLabel,
    expanded: !collapsed,
  }
}
