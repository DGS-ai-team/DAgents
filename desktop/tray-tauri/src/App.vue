<script setup>
// 托盘壳默认隐藏主窗口；打开控制台后会导航到 Node 同源 /ui/。
</script>

<template>
  <main class="shell-stub">
    <p>DAgents Shell 正在准备控制台…</p>
    <p>请使用托盘菜单「打开控制台」，或双击托盘图标。</p>
  </main>
</template>

<style>
html,
body,
#app {
  margin: 0;
  height: 100%;
  font-family: "Segoe UI Variable", "Segoe UI", "PingFang SC", "Noto Sans SC", sans-serif;
  background: #202020;
  color: #ffffff;
}
.shell-stub {
  display: grid;
  place-content: center;
  gap: 0.5rem;
  min-height: 100%;
  padding: 1.5rem;
  text-align: center;
  opacity: 0.85;
}
</style>
