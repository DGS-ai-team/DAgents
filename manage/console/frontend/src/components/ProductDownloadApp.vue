<script setup>
import { computed, onMounted, ref, watch } from "vue";
import { fetchPublicReleasePackages } from "../api.js";
import { formatUnix } from "../utils.js";
import brandIcon from "@dagents-brand/brand-icon.png";
import UiIcon from "./UiIcon.vue";

const REPOSITORY = "https://github.com/DGS-ai-team/DAgents";
const RELEASES = `${REPOSITORY}/releases/latest`;
const PLATFORM_ORDER = ["windows-amd64", "linux-amd64", "linux-arm64", "windows-386"];
const PLATFORM_META = {
  "windows-amd64": { label: "Windows", detail: "64 位安装程序", icon: "monitor" },
  "windows-386": { label: "Windows", detail: "32 位安装程序", icon: "monitor" },
  "linux-amd64": { label: "Linux", detail: "x64 程序包", icon: "terminal" },
  "linux-arm64": { label: "Linux", detail: "ARM64 程序包", icon: "terminal" },
};

const packages = ref([]);
const selectedChannel = ref("stable");
const loading = ref(false);
const error = ref("");
const toastMessage = ref("");
let toastTimer;

const channels = computed(() => [...new Set(packages.value.map((pkg) => pkg.channel))].sort());
const channelPackages = computed(() => packages.value.filter((pkg) => pkg.channel === selectedChannel.value));
const presentPlatforms = computed(() => [...new Set(channelPackages.value.map((pkg) => pkg.platform))].sort((a, b) => {
  const ai = PLATFORM_ORDER.indexOf(a);
  const bi = PLATFORM_ORDER.indexOf(b);
  if (ai < 0 && bi < 0) return a.localeCompare(b);
  if (ai < 0) return 1;
  if (bi < 0) return -1;
  return ai - bi;
}));

function compareVersions(left, right) {
  const a = String(left || "").replace(/^v/i, "").split(/[.+-]/);
  const b = String(right || "").replace(/^v/i, "").split(/[.+-]/);
  for (let index = 0; index < Math.max(a.length, b.length); index += 1) {
    const av = a[index] ?? "0";
    const bv = b[index] ?? "0";
    const an = Number(av);
    const bn = Number(bv);
    const result = Number.isFinite(an) && Number.isFinite(bn)
      ? an - bn
      : String(av).localeCompare(String(bv), undefined, { numeric: true, sensitivity: "base" });
    if (result) return result;
  }
  return 0;
}

function latestFor(platform) {
  const candidates = channelPackages.value.filter((pkg) => pkg.platform === platform);
  return candidates.find((pkg) => pkg.is_latest)
    || [...candidates].sort((a, b) => compareVersions(b.version, a.version))[0]
    || null;
}

const latestPackages = computed(() => presentPlatforms.value
  .map((platform) => ({ platform, pkg: latestFor(platform) }))
  .filter((item) => item.pkg));
const heroPackage = computed(() => latestFor("windows-amd64") || latestPackages.value[0]?.pkg || null);
const heroDownloadUrl = computed(() => heroPackage.value ? downloadUrl(heroPackage.value) : "#download");

function formatSize(value) {
  const bytes = Number(value) || 0;
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${bytes} B`;
}

function downloadUrl(pkg) {
  return `/v1/releases/public/packages/${encodeURIComponent(pkg.channel)}/${encodeURIComponent(pkg.platform)}/${encodeURIComponent(pkg.version)}/download`;
}

function packagesFor(platform) {
  return [...channelPackages.value]
    .filter((pkg) => pkg.platform === platform)
    .sort((a, b) => compareVersions(b.version, a.version));
}

async function copyChecksum(pkg) {
  try {
    await navigator.clipboard.writeText(pkg.sha256);
    toastMessage.value = `SHA-256 已复制 · ${pkg.version}`;
  } catch {
    toastMessage.value = `复制失败，请检查浏览器剪贴板权限`;
  }
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => { toastMessage.value = ""; }, 2600);
}

async function loadPackages() {
  loading.value = true;
  error.value = "";
  try {
    const rows = await fetchPublicReleasePackages();
    packages.value = Array.isArray(rows) ? rows : [];
    if (!channels.value.includes(selectedChannel.value)) {
      selectedChannel.value = channels.value.includes("stable") ? "stable" : (channels.value[0] || "");
    }
  } catch (err) {
    error.value = err.status === 503
      ? "下载服务暂不可用，请稍后再试。"
      : (err.message || "暂时无法读取下载版本");
  } finally {
    loading.value = false;
  }
}

watch(channels, (available) => {
  if (available.length && !available.includes(selectedChannel.value)) {
    selectedChannel.value = available.includes("stable") ? "stable" : available[0];
  }
});

onMounted(loadPackages);
</script>

<template>
  <div class="product-site">
    <header class="product-header">
      <a class="product-brand" href="#top" aria-label="DAgents 首页">
        <img :src="brandIcon" alt="" />
        <span>DAgents</span>
      </a>
      <nav class="product-nav" aria-label="页面导航">
        <a href="#features">产品能力</a>
        <a href="#download">下载</a>
        <a :href="`${REPOSITORY}/tree/dev/docs`" target="_blank" rel="noreferrer">文档</a>
      </nav>
      <a class="product-header-github" :href="REPOSITORY" target="_blank" rel="noreferrer">
        <UiIcon name="code-2" :size="16" /> GitHub
      </a>
    </header>

    <main id="top">
      <section class="product-hero">
        <div class="product-hero__glow" aria-hidden="true"></div>
        <div class="product-hero__inner">
          <div class="product-hero-copy">
            <div class="product-kicker"><span></span> 本地优先 · 开源 Agent 工作台</div>
            <h1>让 Agent<br /><em>真正开始做事。</em></h1>
            <p class="product-hero-lead">
              在一个桌面里连接对话、工具和执行环境。让智能体理解任务，也让每一步操作都看得见、控得住。
            </p>
            <div class="product-hero-actions">
              <a class="product-button product-button--primary" :class="{ 'is-disabled': !heroPackage }" :href="heroDownloadUrl" :download="heroPackage?.filename || undefined" :aria-disabled="!heroPackage">
                <UiIcon name="arrow-down" :size="18" />
                {{ heroPackage ? `下载 DAgents · ${heroPackage.version}` : "查看下载版本" }}
              </a>
              <a class="product-button product-button--quiet" href="#features">了解产品能力 <UiIcon name="arrow-down" :size="15" /></a>
            </div>
            <div class="product-hero-meta">
              <span><UiIcon name="shield-check" :size="15" /> 数据默认留在自己的环境</span>
              <span><UiIcon name="server" :size="15" /> Windows 桌面 · 本机 Node</span>
            </div>
          </div>

          <div class="product-hero-art" aria-label="DAgents 桌面工作台示意图">
            <div class="product-orbit product-orbit--one"></div>
            <div class="product-orbit product-orbit--two"></div>
            <div class="product-app-window">
              <div class="product-app-titlebar">
                <div class="product-app-brand"><img :src="brandIcon" alt="" /><span>DAgents</span></div>
                <div class="product-app-menu"><span>资源</span><span>视图</span><span>智能体</span></div>
                <div class="product-window-dots"><i></i><i></i><i></i></div>
              </div>
              <div class="product-app-body">
                <aside class="product-app-sidebar">
                  <div class="product-side-label">智能体</div>
                  <div class="product-agent-row is-active"><span class="product-agent-avatar">✳</span><span>项目助手</span><i></i></div>
                  <div class="product-agent-row"><span class="product-agent-avatar">✳</span><span>代码审查</span></div>
                  <div class="product-side-label product-side-label--second">自主智能体</div>
                  <div class="product-agent-row"><span class="product-agent-avatar is-auto">✦</span><span>发布监控</span></div>
                  <div class="product-side-footer"><span></span> Node 已连接</div>
                </aside>
                <section class="product-app-chat">
                  <div class="product-chat-tabs"><span class="is-selected"><UiIcon name="message-square" :size="13" /> 项目助手</span><span><UiIcon name="terminal" :size="13" /> 终端 · bash</span><span><UiIcon name="file" :size="13" /> README.md</span></div>
                  <div class="product-chat-inner">
                    <div class="product-chat-date">今天 · 10:42</div>
                    <div class="product-user-message">检查一下最近的构建结果，并整理失败原因。</div>
                    <div class="product-assistant-message">
                      <b>项目助手</b>
                      <p>我先查看了构建日志，失败来自前端依赖未安装。接下来我会检查 lockfile 和构建脚本。</p>
                    </div>
                    <div class="product-tool-run"><span><UiIcon name="terminal" :size="14" /></span><div><b>运行命令</b><small>npm run build</small></div><i>已完成</i></div>
                    <div class="product-chat-compose"><span><UiIcon name="attachment" :size="15" /></span><span>继续向项目助手提问…</span><span class="product-compose-send"><UiIcon name="send" :size="14" /></span></div>
                  </div>
                </section>
              </div>
            </div>
            <div class="product-art-caption"><span><i></i> 当前工作区</span><span>对话、终端与文件在同一处协作</span></div>
          </div>
        </div>
        <a class="product-scroll-cue" href="#features"><span></span> 探索 DAgents</a>
      </section>

      <section id="features" class="product-features section-wrap">
        <div class="product-section-heading">
          <span class="product-overline">一个工作台，多种工作方式</span>
          <h2>从理解任务，到完成任务</h2>
          <p>DAgents 把智能体对话与真实执行环境接在一起，让协作过程清晰、连续。</p>
        </div>
        <div class="product-feature-grid">
          <article class="product-feature-card product-feature-card--wide">
            <span class="product-feature-icon"><UiIcon name="message-square" :size="20" /></span>
            <span class="product-feature-number">01 / 工作台</span>
            <h3>对话和资源，不再分散</h3>
            <p>在同一个工作区切换消息、终端、浏览器与文件。Agent 正在做什么、产出了什么，都能顺手查看。</p>
            <div class="product-resource-pills"><span><UiIcon name="terminal" :size="14" /> 终端</span><span><UiIcon name="browser" :size="14" /> 浏览器</span><span><UiIcon name="file" :size="14" /> 文件</span></div>
          </article>
          <article class="product-feature-card">
            <span class="product-feature-icon"><UiIcon name="shield-check" :size="20" /></span>
            <span class="product-feature-number">02 / 可控执行</span>
            <h3>过程可见，关键操作可审</h3>
            <p>工具调用和执行结果清楚呈现。需要审批时由你决定，权限边界由 Node 执行。</p>
          </article>
          <article class="product-feature-card">
            <span class="product-feature-icon"><UiIcon name="server" :size="20" /></span>
            <span class="product-feature-number">03 / 本机协同</span>
            <h3>Desktop 与 Node 同机协作</h3>
            <p>Desktop 固定连接本机 Node，工作区、会话和任务由 Node 管理；需要操作其他主机时，可使用 Node 的 SSH 或远程终端工具。</p>
          </article>
          <article class="product-feature-card product-feature-card--auto">
            <span class="product-feature-icon"><UiIcon name="auto" :size="20" /></span>
            <span class="product-feature-number">04 / 长期任务</span>
            <h3>让 Auto 持续跟进目标</h3>
            <p>设置唤醒频率与待办，让自主智能体在合适的时机回来检查进展、继续推进。</p>
            <div class="product-auto-track"><span>定时唤醒</span><i></i><span>检查待办</span><i></i><span>继续工作</span></div>
          </article>
        </div>
      </section>

      <section id="download" class="product-download-section">
        <div class="section-wrap product-download-inner">
          <div class="product-download-heading">
            <div>
              <span class="product-overline">开始使用</span>
              <h2>下载 DAgents</h2>
              <p>选择你的运行平台。每个已发布版本都附带校验码与版本说明。</p>
            </div>
            <label v-if="channels.length > 1" class="product-channel-select">
              <span>发行渠道</span>
              <select v-model="selectedChannel">
                <option v-for="channel in channels" :key="channel" :value="channel">{{ channel === "stable" ? "稳定版 · stable" : channel }}</option>
              </select>
            </label>
          </div>

          <div v-if="error" class="product-download-state product-download-state--error" role="alert">
            <UiIcon name="alert" :size="18" /><span>{{ error }}</span><button type="button" @click="loadPackages">重试</button>
          </div>
          <div v-else-if="loading && !packages.length" class="product-download-state" role="status">正在加载安装包…</div>
          <div v-else-if="!latestPackages.length" class="product-download-empty">
            <span class="product-download-empty__icon"><UiIcon name="package" :size="23" /></span>
            <div><b>{{ packages.length ? "这个渠道暂时没有安装包" : "新版本正在准备中" }}</b><p>可先查看项目发布记录，安装包发布后会自动显示在这里。</p></div>
            <a :href="RELEASES" target="_blank" rel="noreferrer">查看 GitHub Releases <UiIcon name="arrow-up" :size="14" /></a>
          </div>
          <div v-else class="product-release-grid" :class="{ 'is-single': latestPackages.length === 1 }">
            <article v-for="{ platform, pkg } in latestPackages" :key="platform" class="product-release-card">
              <div class="product-release-top"><span class="product-release-platform-icon"><UiIcon :name="PLATFORM_META[platform]?.icon || 'package'" :size="20" /></span><span class="product-release-channel">{{ selectedChannel === "stable" ? "稳定版" : selectedChannel }}</span></div>
              <h3>{{ PLATFORM_META[platform]?.label || platform }}</h3>
              <p class="product-release-detail">{{ PLATFORM_META[platform]?.detail || "程序包" }}</p>
              <div class="product-release-version"><b>{{ pkg.version }}</b><span>{{ formatSize(pkg.size_bytes) }}</span></div>
              <p class="product-release-filename" :title="pkg.filename">{{ pkg.filename }}</p>
              <p v-if="pkg.release_notes" class="product-release-notes">{{ pkg.release_notes }}</p>
              <a class="product-button product-button--primary product-release-download" :href="downloadUrl(pkg)" :download="pkg.filename">
                <UiIcon name="arrow-down" :size="16" /> 下载 {{ PLATFORM_META[platform]?.label || platform }}
              </a>
              <button class="product-checksum" type="button" :title="`复制 SHA-256：${pkg.sha256}`" @click="copyChecksum(pkg)">
                <span>SHA-256</span><code>{{ pkg.sha256.slice(0, 12) }}…</code><span class="product-copy-label">复制</span>
              </button>
              <details v-if="packagesFor(platform).length > 1" class="product-previous-versions">
                <summary>查看往期版本 <span>({{ packagesFor(platform).length - 1 }})</span></summary>
                <div v-for="oldPkg in packagesFor(platform).filter((item) => item.version !== pkg.version)" :key="oldPkg.version" class="product-previous-row">
                  <div><b>{{ oldPkg.version }}</b><small>{{ formatSize(oldPkg.size_bytes) }} · {{ formatUnix(oldPkg.published_at || oldPkg.created_at) }}</small><p v-if="oldPkg.release_notes">{{ oldPkg.release_notes }}</p></div>
                  <a :href="downloadUrl(oldPkg)" :download="oldPkg.filename" :aria-label="`下载 ${oldPkg.version}`"><UiIcon name="arrow-down" :size="15" /></a>
                </div>
              </details>
            </article>
          </div>
          <p class="product-download-footnote"><UiIcon name="shield-check" :size="15" /> 下载前可使用 SHA-256 校验文件完整性。安装包仅展示已发布版本。</p>
        </div>
      </section>
    </main>

    <footer class="product-footer">
      <a class="product-brand product-brand--footer" href="#top"><img :src="brandIcon" alt="" /><span>DAgents</span></a>
      <span>本地优先的 AI Agent 工作台 · 开源构建</span>
      <div><a :href="REPOSITORY" target="_blank" rel="noreferrer">GitHub</a><a href="/console/">Manage 管理台</a></div>
    </footer>
    <Transition name="product-toast"><div v-if="toastMessage" class="product-toast" role="status">{{ toastMessage }}</div></Transition>
  </div>
</template>
