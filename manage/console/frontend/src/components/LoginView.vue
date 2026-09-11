<script setup>
import { computed, ref } from "vue";
import brandIcon from "@dagents-brand/brand-icon.png";

const props = defineProps({
  defaultUsername: { type: String, default: "admin" },
  busy: { type: Boolean, default: false },
  error: { type: String, default: "" },
  hint: { type: String, default: "" },
});

const emit = defineEmits(["submit", "node-submit"]);

const username = ref(props.defaultUsername || "admin");
const password = ref("");
const nodeId = ref("");
const nodeToken = ref("");

const canSubmit = computed(() => {
  return Boolean(username.value.trim()) && Boolean(password.value) && !props.busy;
});

function onSubmit() {
  if (!canSubmit.value) return;
  emit("submit", {
    username: username.value.trim(),
    password: password.value,
  });
}

function onNodeSubmit() {
  if (props.busy || !nodeId.value.trim() || !nodeToken.value) return;
  emit("node-submit", { nodeId: nodeId.value.trim(), token: nodeToken.value });
}
</script>

<template>
  <div class="login-page">
    <aside class="login-aside" aria-hidden="true">
      <div class="login-aside-brand">
        <span class="brand-logo">
          <img :src="brandIcon" alt="" />
        </span>
        <strong>DAgents Manage</strong>
      </div>
      <div class="login-aside-copy">
        <h1>集中管理台</h1>
        <p>查看活跃 Node 与工作组，分发配置与案例，让多节点协作保持一致。</p>
      </div>
      <div class="login-aside-meta">Registry · Workgroup · Console</div>
    </aside>

    <div class="login-panel">
      <div class="login-card">
        <div class="login-brand">
          <h1>管理员登录</h1>
          <p>使用管理员账号进入管理台</p>
        </div>

        <p v-if="hint" class="login-hint">{{ hint }}</p>
        <p v-if="error" class="login-error" role="alert">{{ error }}</p>

        <form @submit.prevent="onSubmit">
        <label class="login-field">
          <span>账号</span>
          <input v-model="username" type="text" name="username" autocomplete="username" autofocus />
        </label>
        <label class="login-field">
          <span>密码</span>
          <input
            v-model="password"
            type="password"
            name="password"
            autocomplete="current-password"
          />
        </label>

        <button type="submit" class="login-submit" :disabled="!canSubmit">
          {{ busy ? "登录中…" : "登录" }}
        </button>
        </form>

        <details class="login-node-login">
          <summary>使用 Node 凭据登录</summary>
          <form @submit.prevent="onNodeSubmit">
          <label class="login-field">
            <span>Node ID</span>
            <input v-model="nodeId" type="text" autocomplete="off" />
          </label>
          <label class="login-field">
            <span>Node token</span>
            <input v-model="nodeToken" type="password" autocomplete="off" />
          </label>
          <button type="submit" class="login-submit" :disabled="props.busy || !nodeId.trim() || !nodeToken">
            {{ busy ? "登录中…" : "Node 登录" }}
          </button>
          </form>
        </details>
      </div>
    </div>
  </div>
</template>
