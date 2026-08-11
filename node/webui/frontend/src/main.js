import { createApp } from "vue";
import App from "./App.vue";
import router from "./router/index.js";
import { initTheme } from "./stores/theme.js";
import brandIcon from "./assets/brand-icon.png";
import "./styles/workbench.css";
import "./styles/overrides.css";

initTheme();

const favicon = document.querySelector('link[rel~="icon"]') || document.createElement("link");
favicon.rel = "icon";
favicon.type = "image/png";
favicon.href = brandIcon;
document.head.appendChild(favicon);

createApp(App).use(router).mount("#app");
