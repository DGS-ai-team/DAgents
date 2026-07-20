import { createRouter, createWebHistory } from "vue-router";
import ChatLayout from "../layouts/ChatLayout.vue";
import SettingsLayout from "../layouts/SettingsLayout.vue";
import ChatView from "../views/ChatView.vue";
import GeneralSettings from "../views/settings/GeneralSettings.vue";
import SkillsSettings from "../views/settings/SkillsSettings.vue";
import TriggersSettings from "../views/settings/TriggersSettings.vue";
import SecuritySettings from "../views/settings/SecuritySettings.vue";
import AboutSettings from "../views/settings/AboutSettings.vue";
import ContextSettings from "../views/settings/ContextSettings.vue";
import ConnectionSettings from "../views/settings/ConnectionSettings.vue";
import CapabilitiesSettings from "../views/settings/CapabilitiesSettings.vue";

const router = createRouter({
  history: createWebHistory("/ui/"),
  routes: [
    { path: "/", redirect: "/agents" },
    {
      path: "/chat/:sessionId?",
      redirect: (to) => {
        const id = String(to.params.sessionId || "").trim();
        return id ? `/agents/${encodeURIComponent(id)}` : "/agents";
      },
    },
    {
      path: "/agents/:agentId?",
      component: ChatLayout,
      children: [{ path: "", name: "agents", component: ChatView }],
    },
    {
      path: "/settings",
      component: SettingsLayout,
      redirect: "/settings/general",
      children: [
        { path: "general", name: "settings-general", component: GeneralSettings },
        { path: "connection", name: "settings-connection", component: ConnectionSettings },
        { path: "capabilities", name: "settings-capabilities", component: CapabilitiesSettings },
        { path: "context", name: "settings-context", component: ContextSettings },
        { path: "skills", name: "settings-skills", component: SkillsSettings },
        { path: "triggers", name: "settings-triggers", component: TriggersSettings },
        { path: "security", name: "settings-security", component: SecuritySettings },
        { path: "help", redirect: "/settings/about" },
        { path: "about", name: "settings-about", component: AboutSettings },
      ],
    },
  ],
});

export default router;
