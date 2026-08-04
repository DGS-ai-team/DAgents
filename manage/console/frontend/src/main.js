import { createApp } from "vue";
import App from "./App.vue";
import { initTheme } from "./theme.js";
import "./assets/main.css";

initTheme();
createApp(App).mount("#app");
