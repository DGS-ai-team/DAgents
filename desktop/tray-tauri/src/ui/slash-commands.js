import { runSlashCommand } from './utils/commands.js'

/** Execute supported slash commands from Desktop's composer. */
export async function dispatchDesktopSlashCommand(text, handlers = {}) {
  const command = runSlashCommand(text)
  if (command.error) {
    handlers.onError?.(command.error)
    return true
  }

  const agentId = String(handlers.agentId || '').trim()
  if (!agentId) {
    handlers.onError?.('请先选择智能体')
    return true
  }

  try {
    if (command.action === 'clear') {
      const cleared = await handlers.clearContext(agentId)
      if (cleared === false) return true
      await handlers.refresh?.(agentId)
      handlers.onNotice?.('已清空对话上下文')
      return true
    }

    if (command.action === 'compress') {
      const result = await handlers.compressContext(agentId)
      await handlers.refresh?.(agentId)
      const status = String(result?.status || '')
      if (status === 'applied') {
        const count = Number(result?.compressed_message_count) || 0
        handlers.onNotice?.(count ? `上下文压缩完成，合并了 ${count} 条消息` : '上下文压缩完成')
      } else if (status === 'noop') {
        handlers.onNotice?.('当前上下文无需压缩')
      } else {
        handlers.onNotice?.(`上下文压缩未完成：${status || '未知结果'}`)
      }
      return true
    }
  } catch (error) {
    handlers.onError?.(String(error?.message || error))
    return true
  }

  return false
}
