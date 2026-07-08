<script setup>
import ProductHeader from "../components/ProductHeader.vue";

defineOptions({ name: "ChatLayout" });
</script>

<template>
  <div class="app">
    <ProductHeader />
    <router-view />
  </div>
</template>
