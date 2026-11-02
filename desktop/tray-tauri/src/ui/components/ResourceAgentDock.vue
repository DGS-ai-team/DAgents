<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import agentIcon from '@shared-icons/desktop/agent.svg?url'
import warningIcon from '@shared-icons/dagents/warning.svg?url'
import loadingIcon from '@shared-icons/desktop/loading.svg?url'
import errorIcon from '@shared-icons/desktop/error.svg?url'
import sendIcon from '@shared-icons/desktop/send.svg?url'
import stopIcon from '@shared-icons/dagents/stop.svg?url'
import chevronDownIcon from '@shared-icons/desktop/chevron-down.svg?url'
import chevronUpIcon from '@shared-icons/desktop/chevron-up.svg?url'
import messageIcon from '@shared-icons/desktop/message.svg?url'
import UiIcon from '@desktop-ui/components/UiIcon.vue'

const props = defineProps({
  modelValue: { type: String, default: '' },
  expanded: { type: Boolean, default: false },
  collapsed: { type: Boolean, default: false },
  collapsedInToolbar: { type: Boolean, default: false },
  status: { type: String, default: 'offline', validator: value => ['offline', 'pending', 'generating', 'idle'].includes(value) },
  agentName: { type: String, default: '当前智能体' },
  canSend: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  canStop: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  failedDraft: { type: Boolean, default: false },
  restoreDisabled: { type: Boolean, default: false },
  references: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:modelValue', 'update:expanded', 'update:collapsed', 'send', 'stop', 'restore', 'remove-reference', 'back-to-chat'])
const panel = ref(null)
const toggleButton = ref(null)
const collapsedToggle = ref(null)
const messageInput = ref(null)
const collapsed = computed(() => props.collapsed)
const draft = computed(() => String(props.modelValue || ''))
const statusCopy = computed(() => ({
  offline: '离线', pending: '有待处理的审批', generating: props.cancelling ? '正在停止' : '生成中', idle: '空闲',
}[props.status]))
const statusIcon = computed(() => ({ offline: errorIcon, pending: warningIcon, generating: loadingIcon, idle: agentIcon }[props.status]))
const statusLabel = computed(() => `${props.agentName || '当前智能体'}：${statusCopy.value}`)
const iconStyle = icon => ({ maskImage: `url("${icon}")`, WebkitMaskImage: `url("${icon}")` })
const showStop = computed(() => props.status === 'generating' || props.canStop)
const hasReferences = computed(() => props.references.length > 0)
const actionIcon = computed(() => showStop.value ? stopIcon : sendIcon)
const actionLabel = computed(() => showStop.value ? (props.cancelling ? '正在停止本轮' : '停止本轮') : '发送消息')
const actionDisabled = computed(() => showStop.value
  ? !props.canStop || props.cancelling
  : props.disabled || !props.canSend || (!draft.value.trim() && !hasReferences.value))
function referenceTitle(reference) {
  return [reference.path, reference.anchor?.label, reference.annotation].filter(Boolean).join('\n')
}
function referenceLabel(reference) {
  return `${reference.name} · ${reference.anchor?.label || '整个文件'}`
}

function setExpanded(value) {
  emit('update:expanded', Boolean(value))
}

async function setCollapsed(value) {
  emit('update:collapsed', Boolean(value))
  await nextTick()
  focusVisibleBarControl()
}

function focusVisibleBarControl() {
  if (collapsed.value) {
    if (!props.collapsedInToolbar) collapsedToggle.value?.focus()
    return
  }
  messageInput.value?.focus()
}

function submit() {
  if (showStop.value || actionDisabled.value) return
  emit('send')
}

function stop() {
  if (!showStop.value || !props.canStop || props.cancelling) return
  emit('stop')
}

function makePanelInert(element) {
  element.inert = true
  element.setAttribute('inert', '')
  element.setAttribute('aria-hidden', 'true')
}

watch(() => props.expanded, async (expanded, previous) => {
  if (expanded || !previous) return
  const focused = document.activeElement
  if (focused && panel.value?.contains(focused)) {
    await nextTick()
    toggleButton.value?.focus()
  }
})

onBeforeUnmount(() => {
  if (panel.value?.contains(document.activeElement)) document.activeElement.blur?.()
})
</script>

<template>
  <aside class="terminal-agent-dock resource-agent-dock" :class="{ expanded, 'is-collapsed': collapsed, 'is-collapsed-in-toolbar': collapsed && collapsedInToolbar }">
    <Transition name="dock-panel" @before-leave="makePanelInert">
      <section
        v-if="expanded"
        id="resource-agent-dock-details"
        ref="panel"
        class="dock-details-panel"
        aria-label="智能体消息与审批"
        :aria-hidden="!expanded"
        :inert="!expanded"
        tabindex="-1"
        @keydown.esc.stop.prevent="setExpanded(false)"
      >
        <div class="dock-details-actions">
          <button
            type="button"
            class="dock-back-to-chat"
            :aria-label="`${agentName}：返回完整对话`"
            :title="`${agentName}：返回完整对话`"
            @click="emit('back-to-chat')"
          ><span class="icon-mask" :style="iconStyle(messageIcon)" aria-hidden="true"></span></button>
        </div>
        <div class="dock-details-content"><slot name="details" /></div>
      </section>
    </Transition>

    <div v-if="!collapsed && references.length" class="dock-references" role="list" aria-label="已引用资源">
      <div v-for="reference in references" :key="reference.id" class="dock-reference" role="listitem" :title="referenceTitle(reference)">
        <UiIcon name="paperclip" :size="14" aria-hidden="true" />
        <span>{{ referenceLabel(reference) }}</span>
        <button type="button" :aria-label="`移除引用：${reference.name}`" title="移除引用" @click.stop="emit('remove-reference', reference.id)"><UiIcon name="close" :size="13" /></button>
      </div>
    </div>

    <Transition name="dock-bar" mode="out-in" @after-enter="focusVisibleBarControl">
      <form v-if="!collapsed" key="expanded" id="resource-agent-dock-input" class="dock-input-surface" @submit.prevent="submit">
        <span class="icon-mask dock-status-icon" :class="`is-${status}`" role="img" :aria-label="statusLabel" :title="statusLabel" :style="iconStyle(statusIcon)"></span>
        <input
          ref="messageInput"
          class="dock-message-input"
          :value="draft"
          :disabled="disabled"
          :aria-label="`向${agentName || '当前智能体'}发送消息`"
          placeholder="发消息…"
          @input="emit('update:modelValue', $event.target.value)"
        >
        <button v-if="failedDraft" type="button" class="dock-restore" :disabled="restoreDisabled" aria-label="恢复失败消息" title="恢复失败消息" @click="emit('restore')">恢复</button>
        <button ref="toggleButton" type="button" class="dock-expand-toggle" :aria-label="expanded ? '收起智能体消息区' : '展开智能体消息区'" :title="expanded ? '收起消息区' : status === 'pending' ? '展开消息区，查看待审批请求' : '展开消息区'" aria-controls="resource-agent-dock-details" :aria-expanded="expanded" @click="setExpanded(!expanded)"><span class="icon-mask" :style="iconStyle(expanded ? chevronDownIcon : chevronUpIcon)" aria-hidden="true"></span></button>
        <button type="button" class="dock-collapse-toggle" aria-label="收起快速消息栏" title="收起消息栏" aria-expanded="true" @click="setCollapsed(true)"><span class="icon-mask" :style="iconStyle(chevronDownIcon)" aria-hidden="true"></span></button>
        <button :type="showStop ? 'button' : 'submit'" class="dock-action" :class="{ 'is-stop': showStop }" :disabled="actionDisabled" :aria-label="actionLabel" :title="actionLabel" :aria-busy="cancelling" @click="showStop && stop()"><span class="icon-mask" :style="iconStyle(actionIcon)" aria-hidden="true"></span></button>
      </form>
      <button v-else-if="!collapsedInToolbar" key="collapsed" ref="collapsedToggle" type="button" class="dock-collapsed-toggle" :aria-label="`${statusLabel}；展开快速消息栏`" title="展开快速消息栏" :aria-expanded="false" @click="setCollapsed(false)">
        <span class="icon-mask dock-status-icon" :class="`is-${status}`" aria-hidden="true" :style="iconStyle(statusIcon)"></span>
        <span class="icon-mask dock-collapsed-chevron" :style="iconStyle(chevronUpIcon)" aria-hidden="true"></span>
      </button>
      <span v-else key="collapsed-in-toolbar" class="dock-collapsed-placeholder" aria-hidden="true"></span>
    </Transition>
  </aside>
</template>
