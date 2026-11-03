<script setup>
import UpdatePanel from "../../components/UpdatePanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
</script>

<template>
  <div class="settings-page settings-embedded">
    <SettingsPageHeader
      title="关于与帮助"
      description="查看当前版本与更新渠道；需要更新时按页面提示操作。"
    />

    <section class="settings-section">
      <div class="settings-section__head">
        <div>
          <h2 class="settings-section__title">版本与更新</h2>
          <p class="settings-section__desc">检查本地服务的版本状态和可用升级方式。</p>
        </div>
      </div>
      <UpdatePanel embedded @close="() => {}" />
    </section>
  </div>
</template>
