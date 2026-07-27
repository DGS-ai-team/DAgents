<script setup>
// 托盘壳默认隐藏主窗口；此页仅作开发态占位。
</script>

<template>
  <main class="shell-stub">
    <p>DAgents Shell 以系统托盘运行。</p>
    <p>请使用托盘菜单，或双击托盘图标打开控制台。</p>
  </main>
</template>

<style>
html,
body,
#app {
  margin: 0;
  height: 100%;
  font-family: "Segoe UI", "PingFang SC", "Noto Sans SC", sans-serif;
  background: #0f1419;
  color: #e7ecf3;
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
