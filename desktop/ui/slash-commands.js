import { runSlashCommand } from '../../node/webui/frontend/src/utils/commands.js'

const isEnabledValue = value => ['on', 'enabled', 'true', '1'].includes(value)
const isDisabledValue = value => ['off', 'disabled', 'false', '0'].includes(value)

/** Execute the same slash commands as the Web UI from Desktop's custom composer. */
export async function dispatchDesktopSlashCommand(text, handlers = {}) {
  const command = await runSlashCommand(text)
  if (command.error) {
    handlers.onError?.(command.error)
    return true
  }

  const agentId = String(handlers.agentId || '').trim()
  if (!agentId) {
    handlers.onError?.('请先选择 Agent')
    return true
  }

  try {
    if (command.action === 'clear') {
      await handlers.clearContext(agentId)
      await handlers.refresh?.(agentId)
      handlers.onNotice?.('已清空对话上下文，并终止未完成命令与临时子 Agent')
      return true
    }

    if (command.action === 'compress') {
      const result = await handlers.compressContext(agentId)
      await handlers.refresh?.(agentId)
      const status = String(result?.status || '')
      handlers.onNotice?.(
        status && !['applied', 'done'].includes(status)
          ? `压缩：${status}`
          : '已触发上下文压缩',
      )
      return true
    }

    if (command.action === 'thinking') {
      const parts = String(command.arg || '').trim().split(/\s+/)
      if (!parts[0]) {
        const settings = await handlers.getLLMSettings?.()
        handlers.onNotice?.(`thinking: ${settings?.thinking || '-'}`)
        return true
      }

      let patch
      if (isEnabledValue(parts[0])) patch = { thinking: 'enabled' }
      else if (isDisabledValue(parts[0])) patch = { thinking: 'disabled' }
      else if (parts[0] === 'effort' && ['high', 'max'].includes(parts[1])) {
        patch = { reasoning_effort: parts[1] }
      } else {
        handlers.onError?.('用法: /thinking on|off 或 /thinking effort high|max')
        return true
      }

      const settings = await handlers.patchLLMSettings(patch)
      handlers.onThinkingSettings?.(settings)
      handlers.onNotice?.(`thinking: ${settings?.thinking || '-'}`)
      return true
    }

    if (command.action === 'upload') {
      const result = await handlers.upload(command.upload)
      const note = result?.message ? ` — ${result.message}` : ''
      handlers.onNotice?.(
        `已上传 ${result?.kind || command.upload.kind} ${result?.id || command.upload.id}@${result?.version || command.upload.version}（${result?.status || '完成'}${note}）`,
      )
      return true
    }
  } catch (error) {
    handlers.onError?.(String(error?.message || error))
    return true
  }

  return false
}
