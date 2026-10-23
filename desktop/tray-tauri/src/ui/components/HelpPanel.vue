<script setup>
import { computed } from "vue";
import { HELP_SECTIONS, HELP_SHORTCUTS } from "../utils/helpCommands.js";

const props = defineProps({ embedded: { type: Boolean, default: false }, desktop: { type: Boolean, default: false } });
const sections = computed(() => props.desktop
  ? HELP_SECTIONS.map((section) => ({ ...section, items: section.items.map((item) => item.cmd.startsWith("/thinking") ? { ...item, desc: "开启或关闭 thinking 模式；也可使用对话输入区的控制按钮。" } : item) }))
  : HELP_SECTIONS);

const emit = defineEmits(["close", "pick"]);
</script>

<template>
  <section class="panel panel-overlay__card help-panel" :class="{ 'help-panel--embedded': embedded }">
    <header v-if="!embedded" class="panel__header help-panel__header">
      <div>
        <div class="panel__title">命令帮助</div>
        <div class="help-panel__subtitle">在输入框输入以 <code>/</code> 开头的命令</div>
      </div>
      <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
    </header>

    <div class="panel__body help-panel__body">
      <section class="help-section help-section--shortcuts">
        <h3 class="help-section__title">快捷键</h3>
        <ul class="help-shortcut-list">
          <li v-for="item in HELP_SHORTCUTS" :key="item.keys" class="help-shortcut-item">
            <kbd class="help-kbd">{{ item.keys }}</kbd>
            <span class="help-shortcut-item__desc">{{ item.desc }}</span>
          </li>
        </ul>
      </section>

      <section v-for="section in sections" :key="section.title" class="help-section">
        <h3 class="help-section__title">{{ section.title }}</h3>
        <ul class="help-cmd-list">
          <li v-for="item in section.items" :key="item.cmd" class="help-cmd-item">
            <button type="button" class="help-cmd-item__cmd" @click="emit('pick', item.cmd)">
              {{ item.cmd }}
            </button>
            <span class="help-cmd-item__desc">{{ item.desc }}</span>
          </li>
        </ul>
      </section>

      <p class="help-panel__foot">点击命令可填入输入框，补全参数后 Enter 发送。</p>
    </div>
  </section>
</template>
