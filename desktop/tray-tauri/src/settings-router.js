import { createRouter, createWebHashHistory } from "vue-router";
const settingsLayout = () => import("@desktop-ui/layouts/SettingsLayout.vue");
const generalSettings = () => import("@desktop-ui/views/settings/GeneralSettings.vue");
const connectionSettings = () => import("@desktop-ui/views/settings/ConnectionSettings.vue");
const mcpSettings = () => import("@desktop-ui/views/settings/McpSettings.vue");
const linuxChannelsSettings = () => import("@desktop-ui/views/settings/LinuxChannelsSettings.vue");
const agentsSettings = () => import("@desktop-ui/views/settings/AgentsSettings.vue");
const agentDetailSettings = () => import("@desktop-ui/views/settings/AgentDetailSettings.vue");
const capabilitiesSettings = () => import("@desktop-ui/views/settings/CapabilitiesSettings.vue");
const contextSettings = () => import("@desktop-ui/views/settings/ContextSettings.vue");
const skillsSettings = () => import("@desktop-ui/views/settings/SkillsSettings.vue");
const triggersSettings = () => import("@desktop-ui/views/settings/TriggersSettings.vue");
const securitySettings = () => import("@desktop-ui/views/settings/SecuritySettings.vue");
const aboutSettings = () => import("@desktop-ui/views/settings/AboutSettings.vue");
const feedbackSettings = () => import("@desktop-ui/views/settings/FeedbackSettings.vue");
const desktopSettings = () => import("./views/DesktopSettings.vue");
const chatRoute = { render: () => null };

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
  { path: "/chat", component: chatRoute }, {
    path: "/settings",
    component: settingsLayout,
    redirect: "/settings/general",
    children: [
      { path: "general", component: generalSettings },
      { path: "desktop", component: desktopSettings },
      { path: "connection", component: connectionSettings },
      { path: "mcp", component: mcpSettings },
      { path: "linux-channels", component: linuxChannelsSettings },
      { path: "agents", name: "settings-agents", component: agentsSettings },
      { path: "agents/:agentId", name: "settings-agent-detail", component: agentDetailSettings },
      { path: "capabilities", component: capabilitiesSettings },
      { path: "context", component: contextSettings },
      { path: "skills", component: skillsSettings },
      { path: "triggers", component: triggersSettings },
      { path: "security", component: securitySettings },
      { path: "about", component: aboutSettings },
      { path: "feedback", component: feedbackSettings },
    ],
  }, { path: "/:pathMatch(.*)*", redirect: "/chat" }],
});

export default router;
