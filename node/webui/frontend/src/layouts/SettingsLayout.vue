<script setup>
import { useRoute } from "vue-router";
import ProductHeader from "../components/ProductHeader.vue";

const route = useRoute();

const nav = [
  { to: "/settings/general", label: "通用" },
  { to: "/settings/connection", label: "连接" },
  { to: "/settings/context", label: "上下文" },
  { to: "/settings/skills", label: "技能" },
  { to: "/settings/triggers", label: "定时任务" },
  { to: "/settings/security", label: "安全" },
  { to: "/settings/help", label: "帮助" },
  { to: "/settings/about", label: "关于" },
];
</script>

<template>
  <div class="app">
    <ProductHeader />
    <div class="settings-layout">
      <nav class="settings-layout__nav">
        <router-link
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="settings-layout__link"
          :class="{ 'settings-layout__link--active': route.path === item.to }"
        >
          {{ item.label }}
        </router-link>
      </nav>
      <main class="settings-layout__main panel">
        <router-view />
      </main>
    </div>
  </div>
</template>
