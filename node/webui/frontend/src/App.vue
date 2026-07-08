<script setup>
import { RouterView } from "vue-router";
import ImageLightbox from "./components/ImageLightbox.vue";

defineOptions({ name: "AppRoot" });
</script>

<template>
  <RouterView v-slot="{ Component }">
    <KeepAlive include="ChatLayout">
      <component :is="Component" />
    </KeepAlive>
  </RouterView>
  <ImageLightbox />
</template>
