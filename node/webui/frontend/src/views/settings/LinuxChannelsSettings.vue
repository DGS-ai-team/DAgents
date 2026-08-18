<script setup>
import { onMounted, reactive, ref } from "vue";
import * as api from "../../api/node.js";
import { notifyConfigurationChanged } from "../../utils/configurationEvents.js";

const channels = ref([]);
const credentials = ref([]);
const loading = ref(false);
const saving = ref(false);
const testing = ref("");
const error = ref("");
const status = ref("");

const credential = reactive({ display_name: "", auth_type: "private_key", secret_ref: "", username_hint: "" });
const channel = reactive({ display_name: "", host: "", port: 22, username: "", credential_id: "", host_key_policy: "known_hosts", host_key_ref: "", remote_shell: "bash", default_cwd: "" });

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
    await api.createLinuxCredential({ ...credential, enabled: true });
    Object.assign(credential, { display_name: "", auth_type: "private_key", secret_ref: "", username_hint: "" });
    await load();
    status.value = "凭据引用已保存";
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
    await api.createLinuxChannel({ ...channel, enabled: true });
    Object.assign(channel, { display_name: "", host: "", port: 22, username: "", credential_id: "", host_key_policy: "known_hosts", host_key_ref: "", remote_shell: "bash", default_cwd: "" });
    await load();
    notifyConfigurationChanged("linux-channels");
    status.value = "Linux 通道已保存";
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
  try {
    const result = await api.testLinuxChannel(item.channel_id);
    status.value = `${item.display_name || item.channel_id}：${result.message || (result.available ? "配置有效" : "不可用")}`;
  } catch (e) {
    error.value = e.message || "测试通道失败";
  } finally {
    testing.value = "";
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
    <div class="linux-settings__head">
      <div>
        <h1 class="settings-page__title">Linux 通道</h1>
        <p class="settings-page__intro">配置多个 Linux SSH 连接，再到智能体设置中选择需要暴露的通道。密码、私钥只通过 secret_ref 在执行时解析，不会进入工具参数或会话上下文。</p>
      </div>
      <button type="button" class="btn btn--ghost btn--sm" :disabled="loading" @click="load">刷新</button>
    </div>

    <p v-if="loading" class="linux-settings__muted">加载中…</p>
    <p v-if="error" class="linux-settings__error">{{ error }}</p>
    <p v-if="status" class="linux-settings__ok">{{ status }}</p>

    <section class="settings-section settings-section--standalone">
      <h2 class="settings-section__title">凭据引用</h2>
      <p class="settings-section__desc">当前支持 password、private_key、ssh_agent；ID由系统自动生成，secret_ref 推荐使用 env:变量名。</p>
      <div class="linux-settings__form">
        <input v-model="credential.display_name" class="settings-field__input" placeholder="显示名称" />
        <select v-model="credential.auth_type" class="settings-field__input"><option value="private_key">private_key</option><option value="password">password</option><option value="ssh_agent">ssh_agent</option></select>
        <input v-model="credential.secret_ref" class="settings-field__input" placeholder="secret_ref，例如 env:SSH_KEY" />
        <button type="button" class="btn btn--primary btn--sm" :disabled="saving || !credential.secret_ref" @click="saveCredential">保存凭据</button>
      </div>
      <div v-if="credentials.length" class="linux-settings__list">
        <div v-for="item in credentials" :key="item.credential_id" class="linux-settings__row">
          <div><strong>{{ item.display_name || item.credential_id }}</strong><small>{{ item.credential_id }} · {{ item.auth_type }} · {{ item.has_secret ? "已配置引用" : "未配置" }}</small></div>
          <button type="button" class="btn btn--ghost btn--sm" @click="api.deleteLinuxCredential(item.credential_id).then(load)">删除</button>
        </div>
      </div>
    </section>

    <section class="settings-section settings-section--standalone">
      <h2 class="settings-section__title">新增 SSH 通道</h2>
      <p class="settings-section__desc">通道 ID由系统自动生成。known_hosts 默认读取 Node 当前用户的 ~/.ssh/known_hosts；也可以填写路径。不要使用 insecure 策略。</p>
      <div class="linux-settings__form linux-settings__form--wide">
        <input v-model="channel.display_name" class="settings-field__input" placeholder="显示名称" />
        <input v-model="channel.host" class="settings-field__input" placeholder="主机或 IP" />
        <input v-model.number="channel.port" type="number" class="settings-field__input" placeholder="端口" />
        <input v-model="channel.username" class="settings-field__input" placeholder="Linux 用户名" />
        <select v-model="channel.credential_id" class="settings-field__input"><option value="">选择凭据</option><option v-for="item in credentials" :key="item.credential_id" :value="item.credential_id">{{ item.display_name || item.credential_id }}</option></select>
        <select v-model="channel.host_key_policy" class="settings-field__input"><option value="known_hosts">known_hosts</option><option value="pinned">pinned</option></select>
        <input v-model="channel.host_key_ref" class="settings-field__input" placeholder="known_hosts 路径或 pinned 指纹" />
        <input v-model="channel.default_cwd" class="settings-field__input" placeholder="默认目录（可选）" />
        <button type="button" class="btn btn--primary btn--sm" :disabled="saving || !channel.host || !channel.username || !channel.credential_id" @click="saveChannel">保存通道</button>
      </div>
    </section>

    <section class="settings-section settings-section--standalone">
      <div class="linux-settings__section-head"><div><h2 class="settings-section__title">已配置通道</h2><p class="settings-section__desc">保存后在智能体详情中绑定；未绑定的通道不会对智能体暴露。</p></div><span class="linux-settings__muted">{{ channels.length }} 个</span></div>
      <div v-if="!channels.length" class="linux-settings__muted">还没有配置 Linux 通道。</div>
      <div v-else class="linux-settings__list">
        <div v-for="item in channels" :key="item.channel_id" class="linux-settings__row">
          <div><strong>{{ item.display_name || item.channel_id }}</strong><small>{{ item.channel_id }} · {{ item.username }}@{{ item.host }}:{{ item.port }} · {{ item.host_key_policy }}</small></div>
          <div class="linux-settings__actions"><button type="button" class="btn btn--ghost btn--sm" :disabled="testing === item.channel_id" @click="testChannel(item)">{{ testing === item.channel_id ? "测试中…" : "测试" }}</button><button type="button" class="btn btn--ghost btn--sm" @click="removeChannel(item)">删除</button></div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.linux-settings__head,.linux-settings__section-head,.linux-settings__actions{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.linux-settings__form{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px;margin-top:14px}.linux-settings__form--wide{grid-template-columns:repeat(auto-fit,minmax(180px,1fr))}.linux-settings__list{display:grid;gap:8px;margin-top:14px}.linux-settings__row{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 12px;border:1px solid var(--color-border);border-radius:9px}.linux-settings__row small{display:block;margin-top:4px;color:var(--color-text-muted);font-size:11px}.linux-settings__muted,.linux-settings__error,.linux-settings__ok{margin:10px 0 0;font-size:12px}.linux-settings__muted{color:var(--color-text-muted)}.linux-settings__error{color:var(--color-danger)}.linux-settings__ok{color:var(--color-success,#3d9a5f)}.settings-section{margin-top:18px}@media(max-width:720px){.linux-settings__head,.linux-settings__section-head,.linux-settings__row{align-items:stretch;flex-direction:column}.linux-settings__actions{justify-content:flex-start}}
</style>
