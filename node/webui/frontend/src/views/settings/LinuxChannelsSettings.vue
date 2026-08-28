<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import * as api from "../../api/node.js";
import { notifyConfigurationChanged } from "../../utils/configurationEvents.js";

const channels = ref([]);
const credentials = ref([]);
const loading = ref(false);
const saving = ref(false);
const testing = ref("");
const error = ref("");
const status = ref("");
const testResult = ref(null);
const showCredentialForm = ref(false);
const showChannelForm = ref(false);
const editingChannelId = ref("");

const credential = reactive({ display_name: "", auth_type: "private_key", secret_mode: "environment", secret_ref: "", secret_value: "", username_hint: "" });
const channel = reactive({
  display_name: "", host: "", port: 22, username: "", credential_id: "",
  host_key_policy: "known_hosts", host_key_ref: "", remote_shell: "bash", default_cwd: "", max_sessions: 4,
});

const credentialAuthMeta = {
  private_key: { label: "SSH 私钥", description: "可直接输入私钥或引用环境变量，Node 不会把私钥放进工具参数。" },
  password: { label: "密码", description: "可直接输入密码，也可以通过环境变量引用。" },
};

const credentialAuthDescription = computed(() => credentialAuthMeta[credential.auth_type]?.description || "请选择认证方式。");
const credentialReady = computed(() => {
  if (credential.secret_mode === "direct") {
    return credential.secret_value.trim().length > 0;
  }
  return Boolean(credential.secret_ref.trim());
});
const hostKeyReady = computed(() => (
  channel.host_key_policy !== "pinned" || channel.host_key_ref.trim().length > 0
));
const channelReady = computed(() => Boolean(
  channel.host.trim() &&
  channel.username.trim() &&
  channel.credential_id &&
  hostKeyReady.value &&
  Number(channel.max_sessions) >= 1 && Number(channel.max_sessions) <= 64,
));

function authTypeLabel(type) {
  return credentialAuthMeta[type]?.label || type || "未知认证";
}

function hostKeyPolicyLabel(policy) {
  return policy === "pinned" ? "固定主机指纹" : "known_hosts";
}

function onHostKeyPolicyChange() {
  channel.host_key_ref = "";
}

function credentialStatusLabel(item) {
  if (item.secret_source === "direct") return "已配置直接密码";
  if (item.secret_source === "environment") return "已配置环境变量";
  return item.has_secret ? "已配置凭据" : "未配置凭据";
}

function resetCredentialForm() {
  Object.assign(credential, { display_name: "", auth_type: "private_key", secret_mode: "environment", secret_ref: "", secret_value: "", username_hint: "" });
}

function onCredentialAuthTypeChange() {
  credential.secret_mode = "environment";
  credential.secret_ref = "";
  credential.secret_value = "";
}

function onCredentialSecretModeChange() {
  credential.secret_ref = "";
  credential.secret_value = "";
}

function resetChannelForm() {
  Object.assign(channel, {
    display_name: "", host: "", port: 22, username: "", credential_id: "",
    host_key_policy: "known_hosts", host_key_ref: "", remote_shell: "bash", default_cwd: "", max_sessions: 4,
  });
}

function openCredentialForm() {
  resetCredentialForm();
  error.value = "";
  status.value = "";
  showCredentialForm.value = true;
}

function closeCredentialForm() {
  if (!saving.value) showCredentialForm.value = false;
}

function openChannelForm(item = null) {
  resetChannelForm();
  editingChannelId.value = item?.channel_id || "";
  if (item) {
    Object.assign(channel, {
      display_name: item.display_name || "",
      host: item.host || "",
      port: item.port || 22,
      username: item.username || "",
      credential_id: item.credential_id || "",
      host_key_policy: item.host_key_policy || "known_hosts",
      host_key_ref: item.host_key_ref || "",
      remote_shell: item.remote_shell || "bash",
      default_cwd: item.default_cwd || "",
      max_sessions: item.max_sessions || 4,
    });
  }
  error.value = "";
  status.value = "";
  showChannelForm.value = true;
}

function closeChannelForm() {
  if (!saving.value) {
    showChannelForm.value = false;
    editingChannelId.value = "";
  }
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [channelResult, credentialResult] = await Promise.all([api.listLinuxChannels(), api.listLinuxCredentials()]);
    channels.value = Array.isArray(channelResult?.channels) ? channelResult.channels : [];
    credentials.value = Array.isArray(credentialResult?.credentials) ? credentialResult.credentials : [];
  } catch (e) {
    error.value = e.message || "加载 Linux 通道失败";
  } finally {
    loading.value = false;
  }
}

async function saveCredential() {
  saving.value = true;
  error.value = "";
  status.value = "";
  try {
    const payload = {
      display_name: credential.display_name,
      auth_type: credential.auth_type,
      username_hint: credential.username_hint,
      enabled: true,
    };
    if (credential.secret_mode === "direct") {
      payload.secret_value = credential.secret_value;
    } else {
      payload.secret_ref = credential.secret_ref;
    }
    await api.createLinuxCredential(payload);
    resetCredentialForm();
    showCredentialForm.value = false;
    await load();
    status.value = "凭据已保存";
  } catch (e) {
    error.value = e.message || "保存凭据失败";
  } finally {
    saving.value = false;
  }
}

async function saveChannel() {
  saving.value = true;
  error.value = "";
  status.value = "";
  try {
    if (editingChannelId.value) {
      await api.patchLinuxChannel(editingChannelId.value, { ...channel, enabled: true });
    } else {
      await api.createLinuxChannel({ ...channel, enabled: true });
    }
    resetChannelForm();
    showChannelForm.value = false;
    const wasEditing = Boolean(editingChannelId.value);
    editingChannelId.value = "";
    await load();
    notifyConfigurationChanged("linux-channels");
    status.value = wasEditing ? "Linux 通道已更新" : "Linux 通道已保存";
  } catch (e) {
    error.value = e.message || "保存通道失败";
  } finally {
    saving.value = false;
  }
}

async function testChannel(item) {
  if (!item?.channel_id || testing.value) return;
  testing.value = item.channel_id;
  error.value = "";
  testResult.value = null;
  try {
    const result = await api.testLinuxChannel(item.channel_id);
    testResult.value = { channelId: item.channel_id, ...result };
    status.value = `${item.display_name || item.channel_id}：${result.message || (result.available ? "配置有效" : "不可用")}`;
  } catch (e) {
    error.value = e.message || "测试通道失败";
  } finally {
    testing.value = "";
  }
}

async function pinTestFingerprint() {
  const result = testResult.value;
  const fingerprint = String(result?.host_key_fingerprint || "").trim();
  const channelID = String(result?.channelId || "").trim();
  if (!fingerprint || !channelID || result?.error_code !== "host_key_unknown" || saving.value) return;
  const item = channels.value.find((entry) => entry.channel_id === channelID);
  const label = item?.display_name || item?.host || channelID;
  const confirmed = window.confirm(
    `请先通过可信渠道核对主机指纹：\\n\\n${fingerprint}\\n\\n确认将 ${label} 保存为固定指纹吗？`,
  );
  if (!confirmed) return;
  saving.value = true;
  error.value = "";
  try {
    await api.patchLinuxChannel(channelID, {
      host_key_policy: "pinned",
      host_key_ref: fingerprint,
    });
    const recheck = await api.testLinuxChannel(channelID);
    testResult.value = { channelId: channelID, ...recheck };
    status.value = recheck.available
      ? `${label}：已保存固定指纹并验证成功`
      : `${label}：固定指纹已保存，但复测仍未通过`;
    await load();
  } catch (e) {
    error.value = e.message || "保存主机指纹失败";
  } finally {
    saving.value = false;
  }
}

async function removeChannel(item) {
  if (!item?.channel_id || !window.confirm(`删除通道 ${item.display_name || item.channel_id}？`)) return;
  try {
    await api.deleteLinuxChannel(item.channel_id);
    await load();
    notifyConfigurationChanged("linux-channels");
  } catch (e) {
    error.value = e.message || "删除通道失败";
  }
}

onMounted(() => void load());
</script>

<template>
  <div class="settings-page settings-embedded linux-settings">
    <header class="settings-page__header">
      <div class="settings-page__header-main">
        <h1 class="settings-page__title">Linux 通道</h1>
        <p class="settings-page__intro">先保存认证凭据，再创建 SSH 通道，最后在智能体设置中绑定需要暴露的通道。密码可直接输入或引用环境变量，敏感信息不会进入工具参数或会话上下文。</p>
      </div>
      <div class="settings-page__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
      </div>
    </header>

    <p v-if="loading" class="linux-settings__muted">加载中…</p>
    <p v-if="error" class="linux-settings__error">{{ error }}</p>
    <p v-if="status" class="linux-settings__ok">{{ status }}</p>
    <section v-if="testResult" class="linux-settings__diagnostics" aria-live="polite">
      <div class="linux-settings__diagnostics-head">
        <strong>连接诊断</strong>
        <span v-if="testResult.error_code" class="linux-settings__diagnostics-code">{{ testResult.error_code }}</span>
        <button
          v-if="testResult.error_code === 'host_key_unknown' && testResult.host_key_fingerprint"
          type="button"
          class="btn btn--primary btn--sm"
          :disabled="saving"
          @click="pinTestFingerprint"
        >
          核对后保存为固定指纹
        </button>
      </div>
      <p v-if="testResult.host_key_fingerprint" class="linux-settings__fingerprint">
        观测到的 SSH 主机指纹：<code>{{ testResult.host_key_fingerprint }}</code>
      </p>
      <div class="linux-settings__stages">
        <div
          v-for="stage in testResult.stages || []"
          :key="stage.name"
          class="linux-settings__stage"
          :class="`linux-settings__stage--${stage.status}`"
        >
          <span class="linux-settings__stage-mark" aria-hidden="true">{{ stage.status === "passed" ? "✓" : "!" }}</span>
          <span class="linux-settings__stage-name">{{ stage.name }}</span>
          <span class="linux-settings__stage-message">{{ stage.message || "完成" }}</span>
          <span class="linux-settings__stage-duration">{{ stage.duration_ms || 0 }}ms</span>
        </div>
      </div>
    </section>

    <section class="settings-section settings-section--standalone">
      <div class="settings-section__head">
        <div>
          <h2 class="settings-section__title">凭据清单</h2>
          <p class="settings-section__desc">ID由系统自动生成。密码和 SSH 私钥都可直接输入或使用 <code>env:变量名</code> 引用；直接输入内容会加密存储。</p>
        </div>
        <div class="settings-section__actions">
          <button type="button" class="btn btn--primary btn--sm" @click="openCredentialForm">新增凭据</button>
        </div>
      </div>
      <div v-if="credentials.length" class="linux-settings__list">
        <div v-for="item in credentials" :key="item.credential_id" class="linux-settings__row">
          <div class="linux-settings__row-main"><strong>{{ item.display_name || "未命名凭据" }}</strong><small><code>{{ item.credential_id }}</code> · {{ authTypeLabel(item.auth_type) }} · {{ credentialStatusLabel(item) }}</small></div>
          <button type="button" class="btn btn--ghost btn--sm" @click="api.deleteLinuxCredential(item.credential_id).then(load)">删除</button>
        </div>
      </div>
      <div v-else class="linux-settings__empty settings-empty-state">还没有凭据。点击“新增凭据”开始配置。</div>
    </section>

    <section class="settings-section settings-section--standalone">
      <div class="settings-section__head">
        <div>
          <h2 class="settings-section__title">通道清单</h2>
          <p class="settings-section__desc">保存后在智能体详情中绑定；未绑定的通道不会对智能体暴露。</p>
        </div>
        <div class="settings-section__actions">
          <button type="button" class="btn btn--primary btn--sm" @click="openChannelForm">新增 SSH 通道</button>
        </div>
      </div>
      <div v-if="!channels.length" class="linux-settings__empty settings-empty-state">还没有配置 Linux 通道。</div>
      <div v-else class="linux-settings__list">
        <div v-for="item in channels" :key="item.channel_id" class="linux-settings__row">
          <div class="linux-settings__row-main"><strong>{{ item.display_name || "未命名通道" }}</strong><small><code>{{ item.channel_id }}</code> · {{ item.username }}@{{ item.host }}:{{ item.port }} · {{ hostKeyPolicyLabel(item.host_key_policy) }} · 最大并发会话 {{ item.max_sessions || 1 }}</small></div>
          <div class="linux-settings__actions"><button type="button" class="btn btn--ghost btn--sm" :disabled="testing === item.channel_id" @click="testChannel(item)">{{ testing === item.channel_id ? "测试中…" : "测试" }}</button><button type="button" class="btn btn--ghost btn--sm" @click="openChannelForm(item)">编辑</button><button type="button" class="btn btn--ghost btn--sm" @click="removeChannel(item)">删除</button></div>
        </div>
      </div>
    </section>

    <div v-if="showCredentialForm" class="linux-settings__modal-backdrop" @click.self="closeCredentialForm">
      <section class="linux-settings__modal" role="dialog" aria-modal="true" aria-labelledby="credential-form-title">
        <div class="linux-settings__modal-head">
          <div><h2 id="credential-form-title">新增凭据</h2><p>保存后会自动生成唯一凭据 ID。</p></div>
          <button type="button" class="linux-settings__modal-close" aria-label="关闭" :disabled="saving" @click="closeCredentialForm">×</button>
        </div>
        <div class="linux-settings__editor">
          <div class="linux-settings__field">
            <label>显示名称 <span>可选</span></label>
            <input v-model="credential.display_name" class="settings-field__input" placeholder="例如：生产环境部署密钥" />
          </div>
          <div class="linux-settings__field">
            <label>认证方式 <span>必选</span></label>
            <select v-model="credential.auth_type" class="settings-field__input" @change="onCredentialAuthTypeChange">
              <option value="private_key">SSH 私钥</option>
              <option value="password">密码</option>
            </select>
            <small>{{ credentialAuthDescription }}</small>
          </div>
          <div v-if="credential.auth_type === 'password' || credential.auth_type === 'private_key'" class="linux-settings__field">
            <label>凭据来源 <span>必选</span></label>
            <select v-model="credential.secret_mode" class="settings-field__input" @change="onCredentialSecretModeChange">
              <option value="environment">环境变量引用</option>
              <option value="direct">直接输入</option>
            </select>
            <small>直接输入的内容会加密保存到本机 Node 配置中，不需要重启 Node 或 shell。</small>
          </div>
          <div class="linux-settings__field linux-settings__field--wide">
            <template v-if="credential.secret_mode === 'direct'">
              <label>{{ credential.auth_type === "private_key" ? "SSH 私钥" : "登录密码" }} <span>必选</span></label>
              <textarea v-if="credential.auth_type === 'private_key'" v-model="credential.secret_value" rows="6" class="settings-field__input" placeholder="粘贴 SSH 私钥内容" />
              <input v-else v-model="credential.secret_value" type="password" autocomplete="new-password" class="settings-field__input" placeholder="输入 SSH 登录密码" />
              <small>只会在建立 SSH 连接时使用，不会通过凭据列表接口返回。</small>
            </template>
            <template v-else>
              <label>环境变量引用 <span>必选</span></label>
              <input v-model="credential.secret_ref" class="settings-field__input" :placeholder="credential.auth_type === 'private_key' ? '例如：env:SSH_KEY' : '例如：env:SSH_PASSWORD'" />
              <small>这里只保存引用名称，Node 会在连接时从进程环境变量解析；修改环境变量后仍需重启 Node 才能让进程看到新值。</small>
            </template>
          </div>
        </div>
        <div class="linux-settings__modal-actions">
          <button type="button" class="btn btn--ghost btn--sm" :disabled="saving" @click="closeCredentialForm">取消</button>
          <button type="button" class="btn btn--primary btn--sm" :disabled="saving || !credentialReady" @click="saveCredential">保存凭据</button>
        </div>
      </section>
    </div>

    <div v-if="showChannelForm" class="linux-settings__modal-backdrop" @click.self="closeChannelForm">
      <section class="linux-settings__modal linux-settings__modal--wide" role="dialog" aria-modal="true" aria-labelledby="channel-form-title">
        <div class="linux-settings__modal-head">
          <div><h2 id="channel-form-title">{{ editingChannelId ? "编辑 SSH 通道" : "新增 SSH 通道" }}</h2><p>{{ editingChannelId ? `正在修改通道 ${editingChannelId}` : "通道 ID由系统自动生成，一个通道代表一台远程主机。" }}</p></div>
          <button type="button" class="linux-settings__modal-close" aria-label="关闭" :disabled="saving" @click="closeChannelForm">×</button>
        </div>
        <div class="linux-settings__channel-editor">
          <div class="linux-settings__group">
            <div class="linux-settings__group-head"><strong>连接目标</strong><span>远程主机信息</span></div>
            <div class="linux-settings__grid linux-settings__grid--target">
              <div class="linux-settings__field linux-settings__field--wide"><label>显示名称 <span>可选</span></label><input v-model="channel.display_name" class="settings-field__input" placeholder="例如：生产服务器" /></div>
              <div class="linux-settings__field linux-settings__field--wide"><label>主机或 IP <span>必选</span></label><input v-model="channel.host" class="settings-field__input" placeholder="例如：10.0.0.21" /></div>
              <div class="linux-settings__field"><label>SSH 端口 <span>必选</span></label><input v-model.number="channel.port" type="number" class="settings-field__input" placeholder="22" /></div>
              <div class="linux-settings__field"><label>Linux 用户名 <span>必选</span></label><input v-model="channel.username" class="settings-field__input" placeholder="例如：deploy" /></div>
            </div>
          </div>
          <div class="linux-settings__group">
            <div class="linux-settings__group-head"><strong>登录凭据</strong><span>选择已保存的认证方式</span></div>
            <div class="linux-settings__grid">
              <div class="linux-settings__field"><label>登录凭据 <span>必选</span></label><select v-model="channel.credential_id" class="settings-field__input"><option value="">选择已保存的凭据</option><option v-for="item in credentials" :key="item.credential_id" :value="item.credential_id">{{ item.display_name || "未命名凭据" }}（{{ authTypeLabel(item.auth_type) }}）</option></select></div>
              <div class="linux-settings__field"><label>主机密钥策略 <span>必选</span></label><select v-model="channel.host_key_policy" class="settings-field__input" @change="onHostKeyPolicyChange"><option value="known_hosts">使用 known_hosts</option><option value="pinned">固定 SSH 主机指纹</option></select></div>
              <div class="linux-settings__field linux-settings__field--wide">
                <label>{{ channel.host_key_policy === "pinned" ? "SHA256 主机指纹" : "known_hosts 文件路径" }} <span>{{ channel.host_key_policy === "pinned" ? "必选" : "可选" }}</span></label>
                <input v-model="channel.host_key_ref" class="settings-field__input" :placeholder="channel.host_key_policy === 'pinned' ? '例如：SHA256:xxxxxxxx' : '留空使用 Node 当前用户的 ~/.ssh/known_hosts'" />
                <small v-if="channel.host_key_policy === 'pinned'">首次测试发现未知主机时，可在诊断结果中核对指纹后直接保存。</small>
                <small v-else>Node 会在本机读取该文件；留空时使用当前运行 Node 的系统用户的 SSH known_hosts。</small>
              </div>
            </div>
          </div>
          <div class="linux-settings__group">
            <div class="linux-settings__group-head"><strong>会话默认值</strong><span>可按需调整</span></div>
            <div class="linux-settings__grid">
              <div class="linux-settings__field"><label>默认远程目录 <span>可选</span></label><input v-model="channel.default_cwd" class="settings-field__input" placeholder="例如：/opt/app" /></div>
              <div class="linux-settings__field"><label>远程 Shell <span>可选</span></label><input v-model="channel.remote_shell" class="settings-field__input" placeholder="bash" /></div>
              <div class="linux-settings__field"><label>最大并发会话 <span>必选</span></label><input v-model.number="channel.max_sessions" type="number" min="1" max="64" class="settings-field__input" placeholder="4" /><small>同一 Linux 通道允许同时打开的持久终端数量；智能体绑定时还可以设置更小的上限。</small></div>
            </div>
          </div>
        </div>
        <div class="linux-settings__modal-actions">
          <button type="button" class="btn btn--ghost btn--sm" :disabled="saving" @click="closeChannelForm">取消</button>
          <button type="button" class="btn btn--primary btn--sm" :disabled="saving || !channelReady" @click="saveChannel">{{ editingChannelId ? "保存修改" : "保存 SSH 通道" }}</button>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.linux-settings { max-width: 1180px; }
.linux-settings__actions { display:flex; align-items:center; justify-content:flex-end; gap:10px; flex:0 0 auto; }
.linux-settings__summary { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:12px; margin:22px 0 4px; }
.linux-settings__summary-card { display:flex; flex-direction:column; gap:3px; padding:14px 16px; border:1px solid var(--color-border); border-radius:12px; background:var(--color-surface-muted, #f8fafc); }
.linux-settings__summary-card span,.linux-settings__summary-card small { color:var(--color-text-muted); font-size:12px; }
.linux-settings__summary-card strong { color:var(--color-text); font-size:22px; line-height:1.2; }
.linux-settings__summary-card--hint strong { font-size:18px; }
.linux-settings__count,.linux-settings__step-badge { flex:0 0 auto; padding:4px 9px; border-radius:999px; color:var(--color-text-muted); background:var(--color-surface-muted, #f3f6f8); font-size:12px; }
.linux-settings__step-badge { color:var(--color-primary, #1575c5); background:color-mix(in srgb, var(--color-primary, #1575c5) 10%, transparent); }
.linux-settings__editor,.linux-settings__channel-editor { display:grid; gap:16px; margin-top:18px; }
.linux-settings__editor { grid-template-columns:repeat(2,minmax(0,1fr)); }
.linux-settings__channel-editor { gap:12px; }
.linux-settings__group { padding:16px; border:1px solid var(--color-border); border-radius:12px; background:var(--color-surface-muted, #fbfcfd); }
.linux-settings__group-head { display:flex; align-items:baseline; gap:9px; margin-bottom:13px; }
.linux-settings__group-head strong { font-size:14px; }
.linux-settings__group-head span { color:var(--color-text-muted); font-size:12px; }
.linux-settings__diagnostics { margin:12px 0 16px; padding:12px 14px; border:1px solid var(--color-border); border-radius:12px; background:var(--color-surface-muted, #fbfcfd); }
.linux-settings__diagnostics-head { display:flex; align-items:center; gap:8px; margin-bottom:8px; font-size:13px; }
.linux-settings__diagnostics-code { color:var(--color-danger, #b42318); font-family:var(--font-mono, monospace); font-size:11px; }
.linux-settings__fingerprint { margin:0 0 10px; color:var(--color-text-muted); font-size:12px; line-height:1.5; }
.linux-settings__fingerprint code { color:var(--color-text); word-break:break-all; }
.linux-settings__stages { display:grid; gap:5px; }
.linux-settings__stage { display:grid; grid-template-columns:18px 112px minmax(0, 1fr) auto; align-items:center; gap:7px; min-height:26px; font-size:12px; }
.linux-settings__stage-mark { display:grid; place-items:center; width:16px; height:16px; border-radius:50%; color:#fff; background:var(--color-success, #16803c); font-size:11px; font-weight:700; }
.linux-settings__stage--failed .linux-settings__stage-mark { background:var(--color-danger, #b42318); }
.linux-settings__stage-name { color:var(--color-text); font-weight:600; }
.linux-settings__stage-message { min-width:0; overflow:hidden; color:var(--color-text-muted); text-overflow:ellipsis; white-space:nowrap; }
.linux-settings__stage-duration { color:var(--color-text-muted); font-variant-numeric:tabular-nums; }
.linux-settings__grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:14px; }
.linux-settings__grid--target { grid-template-columns:repeat(4,minmax(0,1fr)); }
.linux-settings__field { min-width:0; display:flex; flex-direction:column; gap:6px; }
.linux-settings__field--wide { grid-column:1 / -1; }
.linux-settings__field label { display:flex; align-items:center; gap:6px; color:var(--color-text); font-size:12px; font-weight:600; }
.linux-settings__field label span { color:var(--color-text-muted); font-size:11px; font-weight:400; }
.linux-settings__field small { color:var(--color-text-muted); font-size:11px; line-height:1.5; }
.linux-settings__field .settings-field__input { width:100%; box-sizing:border-box; }
.linux-settings__form-actions { display:flex; justify-content:flex-end; gap:8px; grid-column:1 / -1; padding-top:2px; }
.linux-settings__agent-note { display:flex; flex-direction:column; gap:4px; padding:11px 13px; border:1px solid color-mix(in srgb, var(--color-primary, #1575c5) 24%, var(--color-border)); border-radius:9px; background:color-mix(in srgb, var(--color-primary, #1575c5) 7%, transparent); color:var(--color-text-muted); font-size:12px; line-height:1.5; }
.linux-settings__agent-note strong { color:var(--color-text); }
.linux-settings__list { display:grid; gap:8px; margin-top:16px; }
.linux-settings__row { display:flex; align-items:center; justify-content:space-between; gap:14px; padding:12px 14px; border:1px solid var(--color-border); border-radius:10px; }
.linux-settings__row-main { min-width:0; }
.linux-settings__row strong { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.linux-settings__row small { display:block; margin-top:5px; overflow:hidden; color:var(--color-text-muted); font-size:11px; text-overflow:ellipsis; white-space:nowrap; }
.linux-settings__row code,.linux-settings code { font-family:ui-monospace,SFMono-Regular,Consolas,monospace; font-size:10px; }
.linux-settings__muted,.linux-settings__error,.linux-settings__ok { margin:10px 0 0; font-size:12px; }
.linux-settings__muted { color:var(--color-text-muted); }
.linux-settings__error { color:var(--color-danger); }
.linux-settings__ok { color:var(--color-success,#3d9a5f); }
.linux-settings__modal-backdrop { position:fixed; inset:0; z-index:50; display:flex; align-items:center; justify-content:center; padding:24px; overflow:auto; background:rgba(15,23,42,.36); }
.linux-settings__modal { width:min(560px,100%); max-height:calc(100vh - 48px); overflow:auto; padding:22px; border:1px solid var(--color-border); border-radius:14px; background:var(--color-surface,#fff); box-shadow:0 20px 60px rgba(15,23,42,.2); }
.linux-settings__modal--wide { width:min(820px,100%); }
.linux-settings__modal-head { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; }
.linux-settings__modal-head h2 { margin:0; color:var(--color-text); font-size:18px; }
.linux-settings__modal-head p { margin:6px 0 0; color:var(--color-text-muted); font-size:12px; line-height:1.5; }
.linux-settings__modal-close { width:28px; height:28px; padding:0; border:0; border-radius:7px; color:var(--color-text-muted); background:transparent; font-size:24px; line-height:1; cursor:pointer; }
.linux-settings__modal-close:hover { color:var(--color-text); background:var(--color-surface-muted,#f3f6f8); }
.linux-settings__modal-actions { display:flex; justify-content:flex-end; gap:8px; margin-top:20px; padding-top:16px; border-top:1px solid var(--color-border); }
@media(max-width:900px) { .linux-settings__grid--target { grid-template-columns:repeat(2,minmax(0,1fr)); } }
@media(max-width:720px) { .linux-settings__row { align-items:stretch; flex-direction:column; } .linux-settings__summary,.linux-settings__editor,.linux-settings__grid,.linux-settings__grid--target { grid-template-columns:1fr; } .linux-settings__actions { justify-content:flex-start; } .linux-settings__form-actions { justify-content:flex-start; } .linux-settings__modal-backdrop { align-items:flex-start; padding:12px; } .linux-settings__modal { padding:16px; max-height:calc(100vh - 24px); } }
@media(max-width:640px) { .linux-settings__stage { grid-template-columns:18px 96px minmax(0, 1fr); } .linux-settings__stage-duration { display:none; } }
</style>
