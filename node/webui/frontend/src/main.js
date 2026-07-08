import { createApp } from "vue";
import App from "./App.vue";
import router from "./router/index.js";
import "./styles/workbench.css";
import "./styles/overrides.css";

createApp(App).use(router).mount("#app");
