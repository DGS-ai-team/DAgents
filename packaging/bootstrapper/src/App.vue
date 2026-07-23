<script setup>
import { computed, onMounted, ref } from "vue";
import { invoke } from "@tauri-apps/api/core";

const STEPS = ["welcome", "path", "options", "install", "done"];

const step = ref("welcome");
const busy = ref(false);
const error = ref("");
const resultMsg = ref("");
const host = ref({
  os: "",
  defaultInstallDir: "",
  hasPayload: false,
  payloadName: null,
  demoMode: true,
});

const installDir = ref("");
const overwritePolicy = ref(false);
const startShell = ref(true);
const openUi = ref(true);

const stepIndex = computed(() => STEPS.indexOf(step.value));

onMounted(async () => {
  try {
    const info = await invoke("get_host_info");
    host.value = info;
    installDir.value = info.defaultInstallDir || "";
  } catch (e) {
    error.value = String(e);
  }
});

function go(next) {
  error.value = "";
  step.value = next;
}

async function browse() {
  error.value = "";
  try {
    const picked = await invoke("pick_install_dir", { current: installDir.value || null });
    if (picked) installDir.value = picked;
  } catch (e) {
    error.value = String(e);
  }
}

async function startInstall() {
  error.value = "";
  resultMsg.value = "";
  if (!installDir.value.trim()) {
    error.value = "请先选择安装目录";
    return;
  }
  go("install");
  busy.value = true;
  try {
    const res = await invoke("run_install", {
      opts: {
        installDir: installDir.value.trim(),
        overwritePolicy: overwritePolicy.value,
        startShell: startShell.value,
        openUi: openUi.value,
      },
    });
    resultMsg.value = res.message || "";
    if (!res.ok) {
      error.value = res.message || "安装失败";
      go("options");
      return;
    }
    if (openUi.value) {
      try {
        await invoke("open_web_ui");
      } catch {
        /* optional */
      }
    }
    go("done");
  } catch (e) {
    error.value = String(e);
    go("options");
  } finally {
    busy.value = false;
  }
}
async function openUiNow() {
  try {
    await invoke("open_web_ui");
  } catch (e) {
    error.value = String(e);
  }
}
</script>

<template>
  <div class="shell">
    <aside class="hero">
      <div>
        <h1 class="brand">DAgents</h1>
        <p class="tagline">本地 Agent 助手安装向导。选好目录，其余交给静默安装引擎。</p>
      </div>
      <p class="hero-foot">Node · Shell · Web UI · 一键落地</p>
    </aside>

    <section class="panel">
      <div class="steps" aria-hidden="true">
        <div
          v-for="(s, i) in STEPS"
          :key="s"
          class="step-dot"
          :class="{ 'is-active': i === stepIndex, 'is-done': i < stepIndex }"
        />
      </div>

      <div class="content">
        <template v-if="step === 'welcome'">
          <span class="badge" :class="{ warn: host.demoMode }">
            {{ host.demoMode ? "演示 / 预览模式" : "已找到安装包" }}
          </span>
          <h2>安装本地助手</h2>
          <p class="lead">
            向导负责体验与选项；实际文件落地仍由 Inno Setup 静默完成，保留权限、卸载与升级兼容性。
          </p>
          <ul class="checklist">
            <li>写入安装目录与默认配置（已有 config 不会覆盖）</li>
            <li>可选启动 Shell 托盘，监护 Agent Node</li>
            <li>打开 Web UI「设置 › 连接」完成 LLM / Manage</li>
          </ul>
          <p v-if="host.payloadName" class="status">payload：{{ host.payloadName }}</p>
          <p v-else class="status">未嵌入 Inno 包时将走演示安装，便于设计预览。</p>
        </template>

        <template v-else-if="step === 'path'">
          <h2>选择位置</h2>
          <p class="lead">建议使用用户目录下的 Programs\DAgents，无需管理员权限。</p>
          <div class="field">
            <label for="dir">安装目录</label>
            <div class="row">
              <input id="dir" v-model="installDir" type="text" spellcheck="false" />
              <button class="btn btn-ghost" type="button" @click="browse">浏览</button>
            </div>
          </div>
        </template>

        <template v-else-if="step === 'options'">
          <h2>安装选项</h2>
          <p class="lead">这些选项在静默安装结束后由向导执行（Inno `/VERYSILENT` 会跳过其自带 [Run]）。</p>
          <div class="toggles">
            <label class="toggle">
              <input v-model="overwritePolicy" type="checkbox" />
              <div>
                <strong>覆盖已有 policy</strong>
                <span>升级时若检测到 .runtime\policy，是否用安装包种子覆盖（默认保留）。</span>
              </div>
            </label>
            <label class="toggle">
              <input v-model="startShell" type="checkbox" />
              <div>
                <strong>安装后启动 Shell</strong>
                <span>后台启动托盘监护（dagents shell --background）。</span>
              </div>
            </label>
            <label class="toggle">
              <input v-model="openUi" type="checkbox" />
              <div>
                <strong>打开 Web UI</strong>
                <span>完成后打开本机设置页，配置模型与连接。</span>
              </div>
            </label>
          </div>
        </template>

        <template v-else-if="step === 'install'">
          <h2>正在安装</h2>
          <p class="lead">请稍候，正在调用安装引擎写入文件与运行时目录。</p>
          <div class="progress" aria-hidden="true"><i /></div>
          <p class="status">{{ busy ? "安装进行中…" : resultMsg || "处理中" }}</p>
        </template>

        <template v-else>
          <h2>安装完成</h2>
          <p class="lead">DAgents 已就绪。可在 Web UI 完成连接配置；Shell 将监护 Node 进程。</p>
          <p class="status ok">{{ resultMsg }}</p>
          <p class="status">安装目录：{{ installDir }}</p>
        </template>

        <p v-if="error" class="status err">{{ error }}</p>
      </div>

      <div class="actions">
        <button
          v-if="step === 'path' || step === 'options'"
          class="btn btn-ghost"
          type="button"
          :disabled="busy"
          @click="go(step === 'path' ? 'welcome' : 'path')"
        >
          上一步
        </button>
        <button
          v-if="step === 'welcome'"
          class="btn btn-primary"
          type="button"
          @click="go('path')"
        >
          开始
        </button>
        <button
          v-else-if="step === 'path'"
          class="btn btn-primary"
          type="button"
          @click="go('options')"
        >
          下一步
        </button>
        <button
          v-else-if="step === 'options'"
          class="btn btn-primary"
          type="button"
          :disabled="busy"
          @click="startInstall"
        >
          安装
        </button>
        <button
          v-else-if="step === 'done'"
          class="btn btn-primary"
          type="button"
          @click="openUiNow"
        >
          打开 Web UI
        </button>
      </div>
    </section>
  </div>
</template>
