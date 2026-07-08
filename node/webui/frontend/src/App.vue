<script setup>
import { RouterView } from "vue-router";

defineOptions({ name: "AppRoot" });
</script>

<template>
  <RouterView v-slot="{ Component }">
    <KeepAlive include="ChatLayout">
      <component :is="Component" />
    </KeepAlive>
  </RouterView>
</template>
