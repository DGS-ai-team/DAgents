import { createApp } from "vue";
import App from "./App.vue";
import router from "./router/index.js";
import { initTheme } from "./stores/theme.js";
import "./styles/workbench.css";
import "./styles/overrides.css";

initTheme();

createApp(App).use(router).mount("#app");
