<script setup>
import { computed, nextTick, onMounted, reactive, ref } from "vue";
import ConfigPanelShell from "./ConfigPanelShell.vue";
import SettingsDisclosure from "./SettingsDisclosure.vue";
import { useSetupConfig } from "../composables/useSetupConfig.js";

const { loading, saving, error, statusMessage, dirty, configPath, configWritable, form, load, save } =
  useSetupConfig();
const rawHistoryEnvControlled = computed(() => form.features.raw_message_history_source === "environment");
const wecomKeyAvailable = computed(() => Boolean(
  form.wecom.webhook_key?.trim()
  || extractWebhookKey(form.wecom.webhook_url)
  || (form.wecom.has_webhook_key && !form.wecom.clear_webhook_key),
));
const wecomApiBaseValid = computed(() => isValidWecomApiBase(form.wecom.api_base));
const panel = ref(null);
const browserSettingsDisclosure = ref(null);
const wecomSettingsDisclosure = ref(null);
const runtimeDisclosure = ref(null);
const diagnosticsDisclosure = ref(null);
const exposeBrowserSettings = ref(false);
const validationErrors = reactive({});
const browserSettingsVisible = computed(() => form.features.browser_enabled || exposeBrowserSettings.value);
const wecomSettingsVisible = computed(() => form.features.wecom_enabled
  || form.wecom.has_webhook_key
  || form.wecom.clear_webhook_key
  || Boolean(form.wecom.webhook_url?.trim() || form.wecom.webhook_key?.trim())
  || Object.keys(validationErrors).some((key) => key.startsWith("wecom.")));

const numericFields = [
  { key: "browser.default_timeout_ms", min: 1000, disclosure: "browser", read: () => form.browser.default_timeout_ms },
  { key: "browser.max_sessions", min: 1, disclosure: "browser", read: () => form.browser.max_sessions },
  { key: "features.skills_max_in_prompt", min: 1, disclosure: "runtime", read: () => form.features.skills_max_in_prompt },
  { key: "features.triggers_poll_seconds", min: 1, disclosure: "runtime", read: () => form.features.triggers_poll_seconds },
  { key: "child_agents.default_ttl_seconds", min: 1, disclosure: "runtime", read: () => form.child_agents.default_ttl_seconds },
  { key: "child_agents.max_ttl_seconds", min: 1, disclosure: "runtime", read: () => form.child_agents.max_ttl_seconds },
  { key: "child_agents.max_active_per_parent", min: 1, disclosure: "runtime", read: () => form.child_agents.max_active_per_parent },
];

const disclosureRefs = {
  browser: browserSettingsDisclosure,
  wecom: wecomSettingsDisclosure,
  runtime: runtimeDisclosure,
  diagnostics: diagnosticsDisclosure,
};

function fieldError(key) {
  return validationErrors[key] || "";
}

function clearFieldError(key) {
  delete validationErrors[key];
}

function extractWebhookKey(value) {
  const raw = String(value || "").trim();
  if (!raw) return "";
  if (!raw.includes("://") && !raw.includes("?") && !raw.includes("/")) return raw;
  try {
    const key = new URL(raw).searchParams.get("key")?.trim() || "";
    return key === "***" ? "" : key;
  } catch {
    return "";
  }
}

function isValidWecomApiBase(value) {
  const raw = String(value || "").trim();
  if (!raw) return true;
  try {
    const parsed = new URL(raw);
    return ["http:", "https:"].includes(parsed.protocol) && Boolean(parsed.hostname);
  } catch {
    return false;
  }
}

async function revealAndFocusField(fieldKey, disclosureKey) {
  if (disclosureKey === "browser" && !form.features.browser_enabled) {
    exposeBrowserSettings.value = true;
  }
  await nextTick();
  await disclosureRefs[disclosureKey]?.value?.open();
  await nextTick();
  const input = panel.value?.$el?.querySelector(`[data-validation-field="${fieldKey}"]`);
  input?.scrollIntoView?.({ block: "center", behavior: "auto" });
  input?.focus();
}

async function validateCapabilities() {
  for (const key of Object.keys(validationErrors)) delete validationErrors[key];
  for (const field of numericFields) {
    const value = Number(field.read());
    if (Number.isInteger(value) && value >= field.min) continue;

    validationErrors[field.key] = field.min === 1000
      ? "请输入不小于 1000 的整数。"
      : "请输入大于 0 的整数。";
    await revealAndFocusField(field.key, field.disclosure);
    return false;
  }

  if (form.features.wecom_enabled) {
    if (!wecomApiBaseValid.value) {
      validationErrors["wecom.api_base"] = "请输入有效的 HTTP(S) 地址。";
      await revealAndFocusField("wecom.api_base", "wecom");
      return false;
    }
    if (!wecomKeyAvailable.value) {
      const clearingExistingKey = form.wecom.clear_webhook_key && form.wecom.has_webhook_key;
      if (clearingExistingKey) {
        validationErrors["wecom.webhook_key"] = "企业微信启用时需要保留或填写新的 Key；如需清除，请先关闭企业微信。";
        await revealAndFocusField("wecom.webhook_key", "wecom");
      } else {
        validationErrors["wecom.webhook_url"] = "请输入包含 key 参数的 Webhook URL，或直接填写 Key。";
        await revealAndFocusField("wecom.webhook_url", "wecom");
      }
      return false;
    }
  }
  return true;
}

async function saveCapabilities() {
  if (!await validateCapabilities()) return;
  // 仅提交此面板拥有的字段，隐藏的进程级总闸不应被表单默认值覆盖。
  const features = {
    browser_enabled: form.features.browser_enabled,
    wecom_enabled: form.features.wecom_enabled,
    skills_max_in_prompt: form.features.skills_max_in_prompt,
    triggers_poll_seconds: form.features.triggers_poll_seconds,
  };
  if (!rawHistoryEnvControlled.value) {
    features.raw_message_history_enabled = form.features.raw_message_history_enabled;
  }
  const wecom = {
    webhook_url: form.wecom.webhook_url || "",
    api_base: form.wecom.api_base || "",
    clear_webhook_key: !!form.wecom.clear_webhook_key,
  };
  if (form.wecom.webhook_key) {
    wecom.webhook_key = form.wecom.webhook_key;
  }
  await save({
    features,
    browser: { ...form.browser },
    wecom,
    child_agents: {
      default_ttl_seconds: form.child_agents.default_ttl_seconds,
      max_ttl_seconds: form.child_agents.max_ttl_seconds,
      max_active_per_parent: form.child_agents.max_active_per_parent,
    },
  });
}

onMounted(load);
</script>

<template>
  <ConfigPanelShell
    ref="panel"
    title="共享服务与运行限制"
    subtitle="配置本机服务及其可用集成；智能体仍需单独授予对应工具权限。"
    :loading="loading"
    :saving="saving"
    :config-path="configPath"
    :config-writable="configWritable"
    :error="error"
    :status-message="statusMessage"
    :dirty="dirty"
    @save="saveCapabilities"
  >
    <section class="settings-section capabilities-config__services">
      <h2 class="settings-section__title">共享服务</h2>
      <p class="settings-section__desc">
        关闭后，对应能力不会出现在智能体创建与设置中。
      </p>
      <div class="capabilities-config__service-list">
        <div class="capabilities-config__service">
          <label class="settings-row">
          <span class="settings-row__copy"><span class="settings-row__title">浏览器服务 <span class="badge badge--beta" title="试验功能">Beta</span></span><span class="settings-row__description">可在获准的工具权限范围内浏览网页和截取页面；当前应用会自动启动并管理浏览器服务。</span></span>
          <input v-model="form.features.browser_enabled" class="settings-switch-input" type="checkbox" aria-label="浏览器服务" />
          </label>
          <SettingsDisclosure
            v-if="browserSettingsVisible"
            ref="browserSettingsDisclosure"
            class="capabilities-config__service-disclosure"
            title="浏览器选项"
          >
            <p class="settings-section__desc">浏览器超时、并发数、路径及证书处理选项。</p>
            <div class="setup-config-panel__field-grid">
              <label class="settings-field">
                <span class="settings-field__label">默认超时（ms）</span>
                <div class="capabilities-config__field-control">
                  <input
                    v-model.number="form.browser.default_timeout_ms"
                    class="settings-field__input"
                    type="number"
                    min="1000"
                    step="1"
                    data-validation-field="browser.default_timeout_ms"
                    :aria-invalid="fieldError('browser.default_timeout_ms') ? 'true' : undefined"
                    @input="clearFieldError('browser.default_timeout_ms')"
                  />
                  <small v-if="fieldError('browser.default_timeout_ms')" class="settings-field__error" role="alert">{{ fieldError('browser.default_timeout_ms') }}</small>
                </div>
              </label>
              <label class="settings-field">
                <span class="settings-field__label">最大并发会话</span>
                <div class="capabilities-config__field-control">
                  <input
                    v-model.number="form.browser.max_sessions"
                    class="settings-field__input"
                    type="number"
                    min="1"
                    step="1"
                    data-validation-field="browser.max_sessions"
                    :aria-invalid="fieldError('browser.max_sessions') ? 'true' : undefined"
                    @input="clearFieldError('browser.max_sessions')"
                  />
                  <small v-if="fieldError('browser.max_sessions')" class="settings-field__error" role="alert">{{ fieldError('browser.max_sessions') }}</small>
                </div>
              </label>
              <label class="settings-field">
                <span class="settings-field__label">截图目录</span>
                <input v-model="form.browser.output_dir" class="settings-field__input" type="text" autocomplete="off" placeholder="browser" />
              </label>
              <label class="settings-field">
                <span class="settings-field__label">Chrome 路径（可选）</span>
                <input v-model="form.browser.chrome_path" class="settings-field__input" type="text" autocomplete="off" placeholder="留空则自动查找" />
              </label>
            </div>
            <label class="settings-row">
              <span class="settings-row__copy"><span class="settings-row__title">显示浏览器窗口</span></span>
              <input v-model="form.browser.headed" class="settings-switch-input" type="checkbox" aria-label="显示浏览器窗口" />
            </label>
            <label class="settings-row">
              <span class="settings-row__copy">
                <span class="settings-row__title">忽略 HTTPS 证书错误</span>
                <span v-if="form.browser.ignore_https_errors" class="settings-row__description" role="status">已启用证书校验例外：浏览器会继续访问证书无效的网站，仅建议用于受控测试环境。</span>
                <span v-else class="settings-row__description">关闭时会正常校验证书；仅在受控测试环境中开启证书例外。</span>
              </span>
              <input v-model="form.browser.ignore_https_errors" class="settings-switch-input" type="checkbox" aria-label="忽略 HTTPS 证书错误" />
            </label>
          </SettingsDisclosure>
        </div>
        <div class="capabilities-config__service">
          <label class="settings-row">
          <span class="settings-row__copy"><span class="settings-row__title">企业微信推送</span><span class="settings-row__description">使用群「消息推送」Webhook；智能体还需单独启用对应工具权限。</span></span>
          <input v-model="form.features.wecom_enabled" class="settings-switch-input" type="checkbox" aria-label="企业微信推送" />
          </label>
          <SettingsDisclosure
            v-if="wecomSettingsVisible"
            ref="wecomSettingsDisclosure"
            class="capabilities-config__service-disclosure"
            title="Webhook 配置"
          >
            <p class="settings-section__desc">配置用于消息推送的 Webhook 地址及 Key。</p>
            <div class="setup-config-panel__field-grid">
              <label class="settings-field">
                <span class="settings-field__label">Webhook 地址或 Key</span>
                <div class="capabilities-config__field-control">
                  <input
                    v-model="form.wecom.webhook_url"
                    class="settings-field__input"
                    type="text"
                    autocomplete="off"
                    placeholder="Webhook URL（含 key）或直接填写 Key"
                    data-validation-field="wecom.webhook_url"
                    :aria-invalid="fieldError('wecom.webhook_url') ? 'true' : undefined"
                    @input="clearFieldError('wecom.webhook_url')"
                  />
                  <small v-if="fieldError('wecom.webhook_url')" class="settings-field__error" role="alert">{{ fieldError('wecom.webhook_url') }}</small>
                </div>
              </label>
              <label class="settings-field">
                <span class="settings-field__label">更新 Key（可选）</span>
                <div class="capabilities-config__field-control">
                  <input
                    v-model="form.wecom.webhook_key"
                    class="settings-field__input"
                    type="password"
                    autocomplete="new-password"
                    :placeholder="form.wecom.has_webhook_key ? '已配置，留空则保留' : '也可只填此项'"
                    data-validation-field="wecom.webhook_key"
                    :aria-invalid="fieldError('wecom.webhook_key') ? 'true' : undefined"
                    @input="clearFieldError('wecom.webhook_key')"
                  />
                  <small v-if="fieldError('wecom.webhook_key')" class="settings-field__error" role="alert">{{ fieldError('wecom.webhook_key') }}</small>
                </div>
              </label>
              <label class="settings-field">
                <span class="settings-field__label">API 基址</span>
                <div class="capabilities-config__field-control">
                  <input
                    v-model="form.wecom.api_base"
                    class="settings-field__input"
                    type="text"
                    autocomplete="off"
                    placeholder="https://qyapi.weixin.qq.com"
                    data-validation-field="wecom.api_base"
                    :aria-invalid="fieldError('wecom.api_base') ? 'true' : undefined"
                    @input="clearFieldError('wecom.api_base')"
                  />
                  <small v-if="fieldError('wecom.api_base')" class="settings-field__error" role="alert">{{ fieldError('wecom.api_base') }}</small>
                </div>
              </label>
            </div>
            <p v-if="!form.features.wecom_enabled && !wecomKeyAvailable" class="capabilities-config__service-note">
              配置 Webhook 后才能发送消息。
            </p>
            <label v-if="form.wecom.has_webhook_key" class="settings-row">
              <span class="settings-row__copy"><span class="settings-row__title">清除已保存的 Webhook Key</span></span>
              <input v-model="form.wecom.clear_webhook_key" class="settings-switch-input" type="checkbox" aria-label="清除已保存的 Webhook Key" />
            </label>
          </SettingsDisclosure>
        </div>
      </div>
    </section>

    <SettingsDisclosure
      ref="runtimeDisclosure"
      class="capabilities-config__group"
      title="运行限制"
    >
      <p class="settings-section__desc">通常无需调整；子智能体工具调用数沿用所属智能体的每轮上限。</p>
      <div class="capabilities-config__subgroup">
        <h3>技能与定时任务</h3>
        <div class="setup-config-panel__field-grid">
          <label class="settings-field">
            <span class="settings-field__label">同时启用技能数上限</span>
            <div class="capabilities-config__field-control">
              <input
                v-model.number="form.features.skills_max_in_prompt"
                class="settings-field__input"
                type="number"
                min="1"
                step="1"
                data-validation-field="features.skills_max_in_prompt"
                :aria-invalid="fieldError('features.skills_max_in_prompt') ? 'true' : undefined"
                @input="clearFieldError('features.skills_max_in_prompt')"
              />
              <small v-if="fieldError('features.skills_max_in_prompt')" class="settings-field__error" role="alert">{{ fieldError('features.skills_max_in_prompt') }}</small>
            </div>
          </label>
          <label class="settings-field">
            <span class="settings-field__label">定时任务检查间隔（秒）</span>
            <div class="capabilities-config__field-control">
              <input
                v-model.number="form.features.triggers_poll_seconds"
                class="settings-field__input"
                type="number"
                min="1"
                step="1"
                data-validation-field="features.triggers_poll_seconds"
                :aria-invalid="fieldError('features.triggers_poll_seconds') ? 'true' : undefined"
                @input="clearFieldError('features.triggers_poll_seconds')"
              />
              <small v-if="fieldError('features.triggers_poll_seconds')" class="settings-field__error" role="alert">{{ fieldError('features.triggers_poll_seconds') }}</small>
            </div>
          </label>
        </div>
      </div>
      <div class="capabilities-config__subgroup">
        <h3>子智能体</h3>
        <p class="settings-section__desc">限制存活时间与每个父智能体的并发数量。</p>
        <div class="setup-config-panel__field-grid">
          <label class="settings-field">
            <span class="settings-field__label">默认存活时间（秒）</span>
            <div class="capabilities-config__field-control">
              <input
                v-model.number="form.child_agents.default_ttl_seconds"
                class="settings-field__input"
                type="number"
                min="1"
                step="1"
                data-validation-field="child_agents.default_ttl_seconds"
                :aria-invalid="fieldError('child_agents.default_ttl_seconds') ? 'true' : undefined"
                @input="clearFieldError('child_agents.default_ttl_seconds')"
              />
              <small v-if="fieldError('child_agents.default_ttl_seconds')" class="settings-field__error" role="alert">{{ fieldError('child_agents.default_ttl_seconds') }}</small>
            </div>
          </label>
          <label class="settings-field">
            <span class="settings-field__label">最大存活时间（秒）</span>
            <div class="capabilities-config__field-control">
              <input
                v-model.number="form.child_agents.max_ttl_seconds"
                class="settings-field__input"
                type="number"
                min="1"
                step="1"
                data-validation-field="child_agents.max_ttl_seconds"
                :aria-invalid="fieldError('child_agents.max_ttl_seconds') ? 'true' : undefined"
                @input="clearFieldError('child_agents.max_ttl_seconds')"
              />
              <small v-if="fieldError('child_agents.max_ttl_seconds')" class="settings-field__error" role="alert">{{ fieldError('child_agents.max_ttl_seconds') }}</small>
            </div>
          </label>
          <label class="settings-field">
            <span class="settings-field__label">每会话并发上限</span>
            <div class="capabilities-config__field-control">
              <input
                v-model.number="form.child_agents.max_active_per_parent"
                class="settings-field__input"
                type="number"
                min="1"
                step="1"
                data-validation-field="child_agents.max_active_per_parent"
                :aria-invalid="fieldError('child_agents.max_active_per_parent') ? 'true' : undefined"
                @input="clearFieldError('child_agents.max_active_per_parent')"
              />
              <small v-if="fieldError('child_agents.max_active_per_parent')" class="settings-field__error" role="alert">{{ fieldError('child_agents.max_active_per_parent') }}</small>
            </div>
          </label>
        </div>
      </div>
    </SettingsDisclosure>

    <SettingsDisclosure
      ref="diagnosticsDisclosure"
      class="capabilities-config__group"
      title="诊断与日志"
    >
      <p class="settings-section__desc">原始消息记录用于排查问题，可能包含对话内容，请按需开启。</p>
      <label class="settings-row">
        <span class="settings-row__copy"><span class="settings-row__title">保存原始消息记录</span><span class="settings-row__description">用于排查问题，可能包含对话内容。</span></span>
        <input v-model="form.features.raw_message_history_enabled" class="settings-switch-input" type="checkbox" aria-label="保存原始消息记录" :disabled="rawHistoryEnvControlled" />
      </label>
      <p v-if="rawHistoryEnvControlled" class="capabilities-config__service-note" role="note">
        当前由环境变量 AGENT_RAW_MESSAGE_HISTORY_ENABLED 控制，设置页只显示有效状态，无法在此修改。
      </p>
    </SettingsDisclosure>
  </ConfigPanelShell>
</template>

<style scoped>
.capabilities-config__service-list {
  display: flex;
  flex-direction: column;
}

.capabilities-config__service {
  padding: 0;
}

.capabilities-config__service:not(:last-child) {
  border-bottom: 1px solid var(--border-subtle);
}

.capabilities-config__service > .settings-row {
  border-bottom: 0;
}

.capabilities-config__service-note {
  margin: 0 0 8px 26px;
  color: var(--color-text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.capabilities-config__service-disclosure {
  margin-left: 26px;
  border-top: 0;
}

.capabilities-config__group {
  margin-top: 4px;
}

.capabilities-config__subgroup {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--border-subtle);
}

.capabilities-config__subgroup h3 {
  margin: 0 0 4px;
  color: var(--color-text);
  font-size: 13px;
  font-weight: 600;
}

.capabilities-config__field-control {
  min-width: 0;
}

.capabilities-config__field-control .settings-field__input {
  width: 100%;
  max-width: 100%;
}

.settings-section__warning {
  margin: 12px 0 0;
  color: var(--color-warning-strong, #a54c00);
  font-size: 12px;
}

@media (max-width: 640px) {
  .capabilities-config__service-note,
  .capabilities-config__service-disclosure {
    margin-left: 0;
  }
}
</style>
