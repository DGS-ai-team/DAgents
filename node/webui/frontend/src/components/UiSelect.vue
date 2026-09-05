<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";

const props = defineProps({
  modelValue: { type: [String, Number], default: "" },
  options: { type: Array, default: () => [] },
  placeholder: { type: String, default: "请选择" },
  disabled: { type: Boolean, default: false },
  /** settings = 设置/首配输入框外观；compact = 稍紧凑 */
  size: { type: String, default: "settings" },
  /** auto = 根据视口空间；above/below = 在表单中固定展开方向 */
  menuPlacement: {
    type: String,
    default: "auto",
    validator: (v) => ["auto", "above", "below"].includes(v),
  },
});

const emit = defineEmits(["update:modelValue", "change"]);

const open = ref(false);
const rootRef = ref(null);
const triggerRef = ref(null);
const menuRef = ref(null);
const menuStyle = ref({});

const normalized = computed(() =>
  (Array.isArray(props.options) ? props.options : [])
    .map((opt) => {
      if (opt == null) return null;
      if (typeof opt === "string" || typeof opt === "number") {
        return { value: String(opt), label: String(opt), disabled: false };
      }
      const value = opt.value ?? opt.id ?? "";
      return {
        value: String(value),
        label: String(opt.label ?? opt.name ?? value),
        disabled: !!opt.disabled,
      };
    })
    .filter(Boolean),
);

const selected = computed(() =>
  normalized.value.find((o) => o.value === String(props.modelValue ?? "")) || null,
);

const displayLabel = computed(() => selected.value?.label || props.placeholder);
const showPlaceholder = computed(() => !selected.value);

function closeMenu() {
  open.value = false;
}

function placeMenu() {
  const el = triggerRef.value;
  if (!el) return;
  const r = el.getBoundingClientRect();
  const gap = 6;
  const maxH = 240;
  const spaceBelow = window.innerHeight - r.bottom - gap;
  const spaceAbove = r.top - gap;
  const openUp =
    props.menuPlacement === "above" ||
    (props.menuPlacement !== "below" && spaceBelow < Math.min(maxH, 120) && spaceAbove > spaceBelow);
  const height = Math.min(maxH, openUp ? spaceAbove : spaceBelow);
  menuStyle.value = {
    position: "fixed",
    left: `${Math.round(r.left)}px`,
    width: `${Math.round(r.width)}px`,
    maxHeight: `${Math.max(120, height)}px`,
    zIndex: 1400,
    ...(openUp
      ? { bottom: `${Math.round(window.innerHeight - r.top + gap)}px`, top: "auto" }
      : { top: `${Math.round(r.bottom + gap)}px`, bottom: "auto" }),
  };
}

async function toggleMenu() {
  if (props.disabled) return;
  if (open.value) {
    closeMenu();
    return;
  }
  open.value = true;
  await nextTick();
  placeMenu();
  menuRef.value?.querySelector('[aria-selected="true"]')?.focus?.();
}

function pick(opt) {
  if (!opt || opt.disabled) return;
  emit("update:modelValue", opt.value);
  emit("change", opt.value);
  closeMenu();
}

function onDocPointerDown(event) {
  if (!open.value) return;
  const root = rootRef.value;
  const menu = menuRef.value;
  const t = event.target;
  if (root?.contains(t) || menu?.contains(t)) return;
  closeMenu();
}

function onKeydown(event) {
  if (!open.value) return;
  if (event.key === "Escape") {
    event.preventDefault();
    closeMenu();
    triggerRef.value?.focus?.();
  }
}

function onWindowChange() {
  if (open.value) placeMenu();
}

watch(open, (v) => {
  if (v) {
    document.addEventListener("pointerdown", onDocPointerDown, true);
    document.addEventListener("keydown", onKeydown, true);
    window.addEventListener("resize", onWindowChange);
    window.addEventListener("scroll", onWindowChange, true);
  } else {
    document.removeEventListener("pointerdown", onDocPointerDown, true);
    document.removeEventListener("keydown", onKeydown, true);
    window.removeEventListener("resize", onWindowChange);
    window.removeEventListener("scroll", onWindowChange, true);
  }
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocPointerDown, true);
  document.removeEventListener("keydown", onKeydown, true);
  window.removeEventListener("resize", onWindowChange);
  window.removeEventListener("scroll", onWindowChange, true);
});
</script>

<template>
  <div
    ref="rootRef"
    class="ui-select"
    :class="[`ui-select--${size}`, { 'ui-select--open': open, 'ui-select--disabled': disabled }]"
  >
    <button
      ref="triggerRef"
      type="button"
      class="ui-select__trigger"
      :disabled="disabled"
      :aria-expanded="open"
      aria-haspopup="listbox"
      @click="toggleMenu"
    >
      <span class="ui-select__value" :class="{ 'ui-select__value--placeholder': showPlaceholder }">
        {{ displayLabel }}
      </span>
      <svg class="ui-select__chevron" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
        <path
          d="M4.5 6.5 8 10l3.5-3.5"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        ref="menuRef"
        class="ui-select__menu"
        role="listbox"
        :style="menuStyle"
      >
        <p v-if="!normalized.length" class="ui-select__empty">暂无可选项</p>
        <button
          v-for="opt in normalized"
          :key="opt.value"
          type="button"
          class="ui-select__option"
          role="option"
          :disabled="opt.disabled"
          :aria-selected="opt.value === String(modelValue ?? '')"
          :class="{ 'ui-select__option--active': opt.value === String(modelValue ?? '') }"
          @click="pick(opt)"
        >
          <span class="ui-select__option-label">{{ opt.label }}</span>
          <span
            v-if="opt.value === String(modelValue ?? '')"
            class="ui-select__option-check"
            aria-hidden="true"
          >✓</span>
        </button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.ui-select {
  position: relative;
  width: 100%;
  max-width: 520px;
}

.ui-select--settings {
  max-width: 520px;
}

.ui-select__trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  min-height: 36px;
  padding: 8px 12px;
  border-radius: var(--radius-md, 8px);
  border: 1px solid var(--color-border);
  background: var(--color-input, var(--color-surface));
  color: var(--color-text);
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s ease, box-shadow 0.15s ease, background 0.15s ease;
}

.ui-select--open .ui-select__trigger,
.ui-select__trigger:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 40%, var(--color-border));
}

.ui-select__trigger:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 45%, transparent);
  outline-offset: 1px;
}

.ui-select__trigger:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.ui-select__value {
  min-width: 0;
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ui-select__value--placeholder {
  color: var(--color-text-subtle);
}

.ui-select__chevron {
  flex: 0 0 auto;
  color: var(--color-text-muted);
  opacity: 0.85;
  transition: transform 0.15s ease;
}

.ui-select--open .ui-select__chevron {
  transform: rotate(180deg);
}
</style>

<!-- menu teleported: unscoped twin via global class names on teleported node -->
<style>
.ui-select__menu {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: 0;
  padding: 6px;
  overflow: auto;
  border-radius: 12px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  box-shadow: var(--shadow-lg, 0 12px 40px rgba(0, 0, 0, 0.12));
  color: var(--color-text);
}

.ui-select__empty {
  margin: 0;
  padding: 12px 10px;
  font-size: 13px;
  color: var(--color-text-subtle);
  text-align: center;
}

.ui-select__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  padding: 9px 10px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.ui-select__option:hover:not(:disabled),
.ui-select__option:focus-visible {
  background: var(--color-surface-hover, color-mix(in srgb, var(--color-text) 6%, transparent));
  outline: none;
}

.ui-select__option--active {
  background: color-mix(in srgb, var(--color-primary-strong, var(--color-primary)) 10%, transparent);
  color: var(--color-text);
}

.ui-select__option:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.ui-select__option-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ui-select__option-check {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--color-primary-strong, var(--color-primary));
}
</style>
