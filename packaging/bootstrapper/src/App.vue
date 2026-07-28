<script setup>
import { computed, onMounted, ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import brandIcon from "./assets/brand-icon.png";

const step = ref("setup"); // setup | install | done
const busy = ref(false);
const error = ref("");
const installDir = ref("");
const demoMode = ref(false);

const canInstall = computed(() => Boolean(installDir.value.trim()) && !busy.value);

const lead = computed(() => {
  if (step.value === "install") return "正在将 DAgents 安装到所选目录…";
  if (step.value === "done") return "安装已完成。点击下方按钮启动托盘程序。";
  if (demoMode.value) return "演示模式：将写入标记文件，不执行真实安装包。";
  return "选择安装目录后开始。安装包会直接落地到该目录（无需先安装本向导）。";
});

onMounted(async () => {
  try {
    const info = await invoke("get_host_info");
    installDir.value = info.defaultInstallDir || "";
    demoMode.value = Boolean(info.demoMode);
  } catch (e) {
    // 浏览器预览（无 Tauri）时静默进入演示态，避免把 invoke 栈刷到 UI。
    const msg = String(e ?? "");
    if (/invoke|Tauri|__TAURI__/i.test(msg)) {
      demoMode.value = true;
      installDir.value = installDir.value || "~/Applications/DAgents";
      return;
    }
    error.value = msg;
  }
});

async function browse() {
  error.value = "";
  try {
    const picked = await invoke("pick_install_dir", {
      current: installDir.value || null,
    });
    if (picked) installDir.value = picked;
  } catch (e) {
    error.value = String(e);
  }
}

async function startInstall() {
  error.value = "";
  if (!installDir.value.trim()) {
    error.value = "请选择安装目录";
    return;
  }
  step.value = "install";
  busy.value = true;
  try {
    const res = await invoke("run_install", {
      opts: {
        installDir: installDir.value.trim(),
        overwritePolicy: false,
        startShell: false,
        openUi: false,
      },
    });
    if (!res.ok) {
      error.value = res.message || "安装失败";
      step.value = "setup";
      return;
    }
    step.value = "done";
  } catch (e) {
    error.value = String(e);
    step.value = "setup";
  } finally {
    busy.value = false;
  }
}

async function openTray() {
  error.value = "";
  try {
    await invoke("open_tray", { installDir: installDir.value.trim() });
  } catch (e) {
    error.value = String(e);
  }
}
</script>

<template>
  <div class="shell">
    <aside class="hero">
      <img class="brand-mark" :src="brandIcon" width="56" height="56" alt="" aria-hidden="true" />
      <div class="brand-text">
        <h1 class="brand">DAgents</h1>
        <p class="brand-sub">本机智能助手</p>
      </div>
    </aside>

    <section class="panel">
      <div class="content">
        <template v-if="step === 'setup'">
          <h2>安装</h2>
          <p class="lead">{{ lead }}</p>
          <div class="field">
            <label for="dir">安装目录</label>
            <div class="row">
              <input id="dir" v-model="installDir" type="text" spellcheck="false" />
              <button class="btn btn-ghost" type="button" @click="browse">浏览</button>
            </div>
          </div>
        </template>

        <template v-else-if="step === 'install'">
          <h2>正在安装</h2>
          <p class="lead">{{ lead }}</p>
          <div class="progress" aria-hidden="true"><i /></div>
        </template>

        <template v-else>
          <h2>安装完成</h2>
          <p class="lead">{{ lead }}</p>
        </template>

        <p v-if="error" class="status err">{{ error }}</p>
      </div>

      <div class="actions">
        <button
          v-if="step === 'setup'"
          class="btn btn-primary"
          type="button"
          :disabled="!canInstall"
          @click="startInstall"
        >
          安装
        </button>
        <button
          v-else-if="step === 'done'"
          class="btn btn-primary"
          type="button"
          @click="openTray"
        >
          打开托盘程序
        </button>
      </div>
    </section>
  </div>
</template>
