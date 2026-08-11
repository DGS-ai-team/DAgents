import { createApp } from "vue";
import App from "./App.vue";
import { initTheme } from "./theme.js";
import brandIcon from "../../../../node/webui/frontend/src/assets/brand-icon.png";
import "./assets/main.css";

initTheme();

const favicon = document.querySelector('link[rel~="icon"]') || document.createElement("link");
favicon.rel = "icon";
favicon.type = "image/png";
favicon.href = brandIcon;
document.head.appendChild(favicon);

createApp(App).mount("#app");
