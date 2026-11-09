<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
  turnError: { type: String, default: '' },
  modelRecoveryAvailable: { type: Boolean, default: false },
  modelName: { type: String, default: '' },
  modelProfile: { type: String, default: '' },
  agentName: { type: String, default: '当前智能体' },
  canSend: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  canStop: { type: Boolean, default: false },
  cancelling: { type: Boolean, default: false },
  failedDraft: { type: Boolean, default: false },
  restoreDisabled: { type: Boolean, default: false },
  references: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:modelValue', 'update:expanded', 'update:collapsed', 'send', 'stop', 'restore', 'remove-reference', 'back-to-chat', 'occupied-height', 'open-model-picker', 'open-model-settings'])
const dockRoot = ref(null)
const panel = ref(null)
const toggleButton = ref(null)
const collapsedToggle = ref(null)
const messageInput = ref(null)
const collapsed = computed(() => props.collapsed)
const detailsVisible = computed(() => props.expanded && !props.collapsed)
const draft = computed(() => String(props.modelValue || ''))
const statusCopy = computed(() => ({
  offline: '离线', pending: '有待处理的审批', generating: props.cancelling ? '正在停止' : '生成中', idle: '空闲',
}[props.status]))
const statusIcon = computed(() => ({ offline: errorIcon, pending: warningIcon, generating: loadingIcon, idle: agentIcon }[props.status]))
const statusLabel = computed(() => props.turnError
  ? `${props.agentName || '当前智能体'}：${props.turnError}`
  : `${props.agentName || '当前智能体'}：${statusCopy.value}`)
const iconStyle = icon => ({ maskImage: `url("${icon}")`, WebkitMaskImage: `url("${icon}")` })
const showStop = computed(() => props.status === 'generating' || props.canStop)
const hasReferences = computed(() => props.references.length > 0)
const actionLabel = computed(() => showStop.value ? (props.cancelling ? '正在停止本轮' : '停止本轮') : '发送消息')
const actionDisabled = computed(() => showStop.value
  ? !props.canStop || props.cancelling
  : props.disabled || !props.canSend || (!draft.value.trim() && !hasReferences.value))
let dockResizeObserver = null
function publishOccupiedHeight() {
  if (props.collapsedInToolbar && props.collapsed && !props.turnError) {
    emit('occupied-height', 0)
    return
  }
  const rootRect = dockRoot.value?.getBoundingClientRect()
  if (!rootRect) return
  const expandedPanelHeight = detailsVisible.value && panel.value ? panel.value.offsetHeight + 8 : 0
  emit('occupied-height', Math.max(0, Math.ceil(rootRect.height + expandedPanelHeight)))
}
async function observeDockSize() {
  await nextTick()
  dockResizeObserver?.disconnect()
  if (dockRoot.value) dockResizeObserver?.observe(dockRoot.value)
  if (detailsVisible.value && panel.value) dockResizeObserver?.observe(panel.value)
  publishOccupiedHeight()
}
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

watch(() => [props.expanded, props.collapsed], async ([expanded, collapsed], [wasExpanded, wasCollapsed]) => {
  const wasVisible = wasExpanded && !wasCollapsed
  const visible = expanded && !collapsed
  if (wasVisible && !visible) {
    const focused = document.activeElement
    if (focused && panel.value?.contains(focused)) {
      await nextTick()
      if (!props.collapsed) toggleButton.value?.focus()
      else if (!props.collapsedInToolbar) collapsedToggle.value?.focus()
    }
  } else if (!wasVisible && visible && wasExpanded && wasCollapsed) {
    // Restore focus when a toolbar-collapsed dock reappears with its expanded
    // details intent preserved.
    await nextTick()
    messageInput.value?.focus()
  }
})

watch(() => [props.expanded, props.collapsed, props.collapsedInToolbar, props.turnError, props.references.length], observeDockSize)

onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') dockResizeObserver = new ResizeObserver(publishOccupiedHeight)
  observeDockSize()
})

onBeforeUnmount(() => {
  dockResizeObserver?.disconnect()
  emit('occupied-height', 0)
  if (panel.value?.contains(document.activeElement)) document.activeElement.blur?.()
})
</script>

<template>
  <aside ref="dockRoot" class="terminal-agent-dock resource-agent-dock" :class="{ expanded, 'is-collapsed': collapsed, 'is-collapsed-in-toolbar': collapsed && collapsedInToolbar }">
    <Transition name="dock-panel" @before-leave="makePanelInert">
      <section
        v-if="detailsVisible"
        id="resource-agent-dock-details"
        ref="panel"
        class="dock-details-panel"
        aria-label="智能体消息与审批"
        :aria-hidden="!detailsVisible"
        :inert="detailsVisible ? null : ''"
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

    <div v-if="!collapsed && references.length" class="dock-references" role="list" :aria-label="`${agentName} 的已引用资源`">
      <span class="dock-reference-target">发送给 {{ agentName }}</span>
      <div v-for="reference in references" :key="reference.id" class="dock-reference" role="listitem" :title="referenceTitle(reference)">
        <UiIcon name="paperclip" :size="14" aria-hidden="true" />
        <span>{{ referenceLabel(reference) }}</span>
        <button type="button" :aria-label="`移除引用：${reference.name}`" title="移除引用" @click.stop="emit('remove-reference', reference.id)"><UiIcon name="close" :size="13" /></button>
      </div>
    </div>

    <div v-if="turnError && !collapsed" class="dock-turn-error">
      <p role="alert">{{ turnError }}</p>
      <span v-if="modelRecoveryAvailable && (modelName || modelProfile)" class="dock-turn-error__model">
        当前选择 · {{ modelName ? `模型：${modelName}` : '' }}{{ modelName && modelProfile ? ' · ' : '' }}{{ modelProfile ? `配置：${modelProfile}` : '' }}
      </span>
      <div v-if="modelRecoveryAvailable" class="dock-turn-error__actions">
        <button type="button" title="切换模型配置" aria-label="切换模型配置" @click="emit('open-model-picker')"><UiIcon name="arrow-left-right" :size="14" /></button>
        <button v-if="modelProfile" type="button" title="打开模型设置" aria-label="打开模型设置" @click="emit('open-model-settings')"><UiIcon name="settings" :size="14" /></button>
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
        <button :type="showStop ? 'button' : 'submit'" class="dock-action composer-action" :class="{ 'is-stop': showStop }" :disabled="actionDisabled" :aria-label="actionLabel" :title="actionLabel" :aria-busy="cancelling" @click="showStop && stop()"><span class="icon-mask composer-action__send" :style="iconStyle(sendIcon)" aria-hidden="true"></span><span class="icon-mask composer-action__stop" :style="iconStyle(stopIcon)" aria-hidden="true"></span></button>
      </form>
      <button v-else-if="!collapsedInToolbar" key="collapsed" ref="collapsedToggle" type="button" class="dock-collapsed-toggle" :aria-label="`${statusLabel}；展开快速消息栏`" title="展开快速消息栏" :aria-expanded="false" @click="setCollapsed(false)">
        <span class="icon-mask dock-status-icon" :class="`is-${status}`" aria-hidden="true" :style="iconStyle(statusIcon)"></span>
        <span class="icon-mask dock-collapsed-chevron" :style="iconStyle(chevronUpIcon)" aria-hidden="true"></span>
      </button>
      <span v-else key="collapsed-in-toolbar" class="dock-collapsed-placeholder" :class="{ 'has-error': turnError }" :title="turnError || undefined" :aria-label="turnError || undefined" :aria-hidden="!turnError" role="alert">
        <span v-if="turnError" class="dock-error-compact">本轮失败</span>
      </span>
    </Transition>
  </aside>
</template>
