<script setup>
import { computed, ref } from "vue";

const props = defineProps({
  defaultUsername: { type: String, default: "admin" },
  busy: { type: Boolean, default: false },
  error: { type: String, default: "" },
  hint: { type: String, default: "" },
});

const emit = defineEmits(["submit"]);

const username = ref(props.defaultUsername || "admin");
const password = ref("");

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
</script>

<template>
  <div class="login-page">
    <form class="login-card" @submit.prevent="onSubmit">
      <div class="login-brand">
        <div class="login-logo" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="24" rx="6" fill="var(--primary)" />
            <path d="M6 17V7h2.4v7H15V17H6zm8.4-5V7H17v5h-2.6z" fill="#fff" />
          </svg>
        </div>
        <h1>DAgents Manage</h1>
        <p>管理员登录</p>
      </div>

      <p v-if="hint" class="login-hint">{{ hint }}</p>
      <p v-if="error" class="login-error" role="alert">{{ error }}</p>

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
  </div>
</template>
