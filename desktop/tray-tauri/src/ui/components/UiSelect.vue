<script setup>
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from "vue";
import UiIcon from "./UiIcon.vue";

const props = defineProps({
  modelValue: { type: [String, Number], default: "" },
  options: { type: Array, default: () => [] },
  placeholder: { type: String, default: "请选择" },
  disabled: { type: Boolean, default: false },
  searchable: { type: Boolean, default: false },
  searchPlaceholder: { type: String, default: "搜索选项" },
  ariaLabel: { type: String, default: "" },
  ariaInvalid: { type: Boolean, default: false },
  /** Keep popovers inside modal focus traps. Positioning still uses the viewport. */
  contained: { type: Boolean, default: false },
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
const searchRef = ref(null);
const query = ref("");
const activeIndex = ref(-1);
const listId = `${useId()}-options`;

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
        description: String(opt.description ?? ""),
        searchText: String(opt.searchText ?? ""),
      };
    })
    .filter(Boolean),
);

const filtered = computed(() => {
  const words = query.value.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  return normalized.value.filter(opt => {
    const text = `${opt.label} ${opt.description || ""} ${opt.searchText || ""}`.toLocaleLowerCase();
    return words.every(word => text.includes(word));
  });
});
const activeOptionId = computed(() => activeIndex.value >= 0 && filtered.value[activeIndex.value]
  ? `${listId}-${activeIndex.value}` : undefined);

const selected = computed(() =>
  normalized.value.find((o) => o.value === String(props.modelValue ?? "")) || null,
);

const displayLabel = computed(() => selected.value?.label || props.placeholder);
const showPlaceholder = computed(() => !selected.value);

function closeMenu() {
  open.value = false;
  query.value = "";
}

function enabledIndices() {
  return filtered.value.flatMap((option, index) => option.disabled ? [] : [index]);
}

function nextEnabledIndex(key, currentIndex) {
  const enabled = enabledIndices();
  if (!enabled.length) return -1;
  if (key === "Home") return enabled[0];
  if (key === "End") return enabled.at(-1);
  const currentPosition = enabled.indexOf(currentIndex);
  const step = key === "ArrowDown" ? 1 : -1;
  return enabled[(currentPosition + step + enabled.length) % enabled.length];
}

function focusOption(index) {
  if (index < 0) return;
  activeIndex.value = index;
  const option = menuRef.value?.querySelectorAll(".ui-select__option")[index];
  option?.focus();
  option?.scrollIntoView?.({ block: "nearest" });
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

async function toggleMenu(openingKey = "") {
  if (props.disabled) return;
  if (open.value) {
    closeMenu();
    return;
  }
  open.value = true;
  query.value = "";
  resetActive();
  if (openingKey === "ArrowUp" && !selected.value) activeIndex.value = enabledIndices().at(-1) ?? -1;
  await nextTick();
  placeMenu();
  if (props.searchable) searchRef.value?.focus();
  else focusOption(activeIndex.value);
}

function pick(opt) {
  if (props.disabled || triggerRef.value?.matches(":disabled") || !opt || opt.disabled) return;
  emit("update:modelValue", opt.value);
  emit("change", opt.value);
  closeMenu();
  triggerRef.value?.focus();
}

function onOptionKeydown(event, index) {
  if (!props.searchable && event.key === "Enter") {
    event.preventDefault();
    pick(filtered.value[index]);
    return;
  }
  if (!props.searchable && event.key === "Tab") {
    closeMenu();
    triggerRef.value?.focus();
    return;
  }
  if (props.searchable || !["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  focusOption(nextEnabledIndex(event.key, index));
}

function resetActive() {
  const selectedIndex = filtered.value.findIndex(opt => !opt.disabled && opt.value === String(props.modelValue ?? ""));
  activeIndex.value = selectedIndex >= 0 ? selectedIndex : enabledIndices()[0] ?? -1;
}

async function onSearchKeydown(event) {
  if (event.isComposing) return;
  if (event.key === "Enter") {
    event.preventDefault();
    pick(filtered.value[activeIndex.value]);
  } else if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    event.preventDefault();
    activeIndex.value = nextEnabledIndex(event.key, activeIndex.value);
    await nextTick();
    document.getElementById(activeOptionId.value)?.scrollIntoView?.({ block: "nearest" });
  } else if (event.key === "Tab") {
    closeMenu();
    triggerRef.value?.focus();
  }
}

function onTriggerKeydown(event) {
  if (!["ArrowDown", "ArrowUp"].includes(event.key)) return;
  event.preventDefault();
  if (!open.value) {
    void toggleMenu(event.key);
  } else if (props.searchable) {
    searchRef.value?.focus();
  } else {
    focusOption(activeIndex.value);
  }
}

watch(filtered, resetActive);
watch(() => props.disabled, value => { if (value) closeMenu(); });

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
    event.stopPropagation();
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
      :aria-label="ariaLabel || undefined"
      :aria-invalid="ariaInvalid ? 'true' : undefined"
      :aria-controls="open ? listId : undefined"
      aria-haspopup="listbox"
      @click="toggleMenu"
      @keydown="onTriggerKeydown"
    >
      <span class="ui-select__value" :class="{ 'ui-select__value--placeholder': showPlaceholder }">
        {{ displayLabel }}
      </span>
      <UiIcon class="ui-select__chevron" name="chevron-down" :size="15" />
    </button>

    <Teleport to="body" :disabled="contained">
      <div
        v-if="open"
        ref="menuRef"
        class="ui-select__menu"
        :style="menuStyle"
      >
        <div v-if="searchable" class="ui-select__search">
          <UiIcon name="search" :size="15" aria-hidden="true" />
          <input
            ref="searchRef"
            v-model="query"
            type="search"
            role="combobox"
            :placeholder="searchPlaceholder"
            :aria-label="searchPlaceholder"
            aria-autocomplete="list"
            aria-expanded="true"
            :aria-controls="listId"
            :aria-activedescendant="activeOptionId"
            autocomplete="off"
            @keydown="onSearchKeydown"
          >
        </div>
        <div :id="listId" class="ui-select__list" role="listbox" :aria-label="ariaLabel || undefined">
        <p v-if="!filtered.length" class="ui-select__empty" role="status">{{ query.trim() ? '无匹配项' : '暂无可选项' }}</p>
        <button
          v-for="(opt, index) in filtered"
          :id="`${listId}-${index}`"
          :key="opt.value"
          type="button"
          class="ui-select__option"
          role="option"
          :tabindex="searchable ? -1 : undefined"
          :disabled="opt.disabled"
          :aria-selected="opt.value === String(modelValue ?? '')"
          :class="{ 'ui-select__option--active': opt.value === String(modelValue ?? ''), 'ui-select__option--focused': searchable && index === activeIndex }"
          @mousedown="searchable && $event.preventDefault()"
          @click="pick(opt)"
          @keydown="onOptionKeydown($event, index)"
        >
          <span class="ui-select__option-copy"><span class="ui-select__option-label">{{ opt.label }}</span><small v-if="opt.description" class="ui-select__option-description">{{ opt.description }}</small></span>
          <span
            v-if="opt.value === String(modelValue ?? '')"
            class="ui-select__option-check"
            aria-hidden="true"
          ><UiIcon name="check" :size="14" /></span>
        </button>
        </div>
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
</style>
