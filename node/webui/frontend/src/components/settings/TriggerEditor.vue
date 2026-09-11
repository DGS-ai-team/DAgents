<script setup>
import {
  TRIGGER_SCHEDULE_OPTIONS,
  WEEKDAY_OPTIONS,
  isCalendarScheduleKind,
} from "../../utils/triggerForm.js";

const form = defineModel("form", { type: Object, required: true });

defineProps({
  formError: { type: String, default: "" },
  busy: { type: Boolean, default: false },
  submitLabel: { type: String, default: "保存" },
  enabledLabel: { type: String, default: "创建后启用" },
  agents: { type: Array, default: () => [] },
  agentsLoading: { type: Boolean, default: false },
});

const emit = defineEmits(["submit", "cancel"]);
</script>

<template>
  <form class="trigger-form" @submit.prevent="emit('submit')">
    <p v-if="formError" class="command-panel__error trigger-form__error">{{ formError }}</p>

    <label class="settings-field">
      <span class="settings-field__label">名称</span>
      <input v-model="form.name" class="settings-field__input" type="text" required autocomplete="off" />
    </label>

    <label class="settings-field">
      <span class="settings-field__label">调度类型</span>
      <select v-model="form.scheduleKind" class="settings-field__input">
        <option v-for="opt in TRIGGER_SCHEDULE_OPTIONS" :key="opt.value" :value="opt.value">
          {{ opt.label }}
        </option>
      </select>
    </label>

    <label class="settings-field">
      <span class="settings-field__label">目标智能体</span>
      <select v-model="form.targetAgentId" class="settings-field__input" required :disabled="agentsLoading">
        <option value="" disabled>{{ agentsLoading ? "正在加载智能体…" : "请选择智能体" }}</option>
        <option v-for="agent in agents" :key="agent.agent_id || agent.id" :value="agent.agent_id || agent.id">
          {{ agent.display_name || agent.name || agent.agent_id || agent.id }}<template v-if="agent.workspace?.path || agent.workspace_path"> · {{ agent.workspace?.path || agent.workspace_path }}</template>
        </option>
      </select>
      <span class="settings-field__hint">任务会在该智能体的工作目录中继续执行。</span>
    </label>

    <label class="settings-field">
      <span class="settings-field__label">会话策略</span>
      <select v-model="form.sessionTargetMode" class="settings-field__input">
        <option value="fixed">固定会话（由系统绑定）</option>
        <option v-if="form.sessionTargetMode !== 'fixed'" :value="form.sessionTargetMode" disabled>
          {{ form.sessionTargetMode === "new_session" ? "每次使用新会话（仅保留原值）" : "使用最近活跃会话（仅保留原值）" }}
        </option>
      </select>
      <span class="settings-field__hint">任务使用目标智能体的主会话。</span>
    </label>

    <label v-if="form.scheduleKind === 'interval'" class="settings-field">
      <span class="settings-field__label">间隔（秒）</span>
      <input
        v-model.number="form.intervalSeconds"
        class="settings-field__input"
        type="number"
        min="1"
        step="1"
        required
      />
    </label>

    <label v-else-if="form.scheduleKind === 'once'" class="settings-field">
      <span class="settings-field__label">执行时间</span>
      <input v-model="form.fireAtLocal" class="settings-field__input" type="datetime-local" required />
    </label>

    <template v-else-if="isCalendarScheduleKind(form.scheduleKind)">
      <div class="trigger-form__row">
        <label class="settings-field trigger-form__field--half">
          <span class="settings-field__label">小时</span>
          <input v-model.number="form.hour" class="settings-field__input" type="number" min="0" max="23" />
        </label>
        <label class="settings-field trigger-form__field--half">
          <span class="settings-field__label">分钟</span>
          <input v-model.number="form.minute" class="settings-field__input" type="number" min="0" max="59" />
        </label>
      </div>

      <label v-if="form.scheduleKind === 'weekly'" class="settings-field">
        <span class="settings-field__label">星期</span>
        <select v-model.number="form.weekday" class="settings-field__input">
          <option v-for="opt in WEEKDAY_OPTIONS" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
        </select>
      </label>

      <label v-else-if="form.scheduleKind === 'monthly'" class="settings-field">
        <span class="settings-field__label">日期（负数=倒数，如 -1 为最后一天）</span>
        <input v-model.number="form.day" class="settings-field__input" type="number" min="-31" max="31" step="1" />
      </label>

      <div v-if="form.cmd" class="trigger-form__legacy-warning">
        <span>该任务包含旧版命令检查，当前不会执行。</span>
        <button type="button" class="btn btn--ghost btn--sm" @click="form.cmd = ''">清除旧检查</button>
      </div>
    </template>

    <label class="settings-field trigger-form__task">
      <span class="settings-field__label">任务模板</span>
      <textarea
        v-model="form.taskTemplate"
        class="settings-field__input trigger-form__textarea"
        rows="4"
        required
        placeholder="触发时投递给 Agent 的任务正文"
      />
    </label>

    <label class="settings-toggle trigger-form__enabled">
      <input v-model="form.enabled" type="checkbox" />
      <span>{{ enabledLabel }}</span>
    </label>

    <div class="trigger-form__actions">
      <button type="submit" class="btn btn--primary btn--sm" :disabled="busy">{{ submitLabel }}</button>
      <button type="button" class="btn btn--ghost btn--sm" :disabled="busy" @click="emit('cancel')">取消</button>
    </div>
  </form>
</template>
