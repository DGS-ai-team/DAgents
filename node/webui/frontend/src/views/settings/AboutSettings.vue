<script setup>
import UpdatePanel from "../../components/UpdatePanel.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">关于</h1>
    <p class="settings-page__hint">版本信息与更新检查。</p>
    <UpdatePanel @close="() => {}" />
  </div>
</template>
