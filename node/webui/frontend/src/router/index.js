import { createRouter, createWebHistory } from "vue-router";
import ChatLayout from "../layouts/ChatLayout.vue";
import SettingsLayout from "../layouts/SettingsLayout.vue";
import ChatView from "../views/ChatView.vue";
import GeneralSettings from "../views/settings/GeneralSettings.vue";
import SkillsSettings from "../views/settings/SkillsSettings.vue";
import TriggersSettings from "../views/settings/TriggersSettings.vue";
import SecuritySettings from "../views/settings/SecuritySettings.vue";
import AboutSettings from "../views/settings/AboutSettings.vue";

const router = createRouter({
  history: createWebHistory("/ui/"),
  routes: [
    { path: "/", redirect: "/chat" },
    {
      path: "/chat/:sessionId?",
      component: ChatLayout,
      children: [{ path: "", name: "chat", component: ChatView }],
    },
    {
      path: "/settings",
      component: SettingsLayout,
      redirect: "/settings/general",
      children: [
        { path: "general", name: "settings-general", component: GeneralSettings },
        { path: "skills", name: "settings-skills", component: SkillsSettings },
        { path: "triggers", name: "settings-triggers", component: TriggersSettings },
        { path: "security", name: "settings-security", component: SecuritySettings },
        { path: "about", name: "settings-about", component: AboutSettings },
      ],
    },
  ],
});

export default router;
