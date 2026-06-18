import { ref } from "vue";

const toasts = ref([]);
let nextId = 1;

export function useToast() {
  function showToast(message, type = "success") {
    const id = nextId++;
    toasts.value.push({ id, message, type, visible: true });
    setTimeout(() => {
      const item = toasts.value.find((t) => t.id === id);
      if (item) item.visible = false;
      setTimeout(() => {
        toasts.value = toasts.value.filter((t) => t.id !== id);
      }, 220);
    }, 3200);
  }

  return { toasts, showToast };
}
