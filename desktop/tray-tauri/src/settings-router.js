import { createRouter, createWebHashHistory } from "vue-router";
const settingsLayout = () => import("../../../node/webui/frontend/src/layouts/SettingsLayout.vue");
const generalSettings = () => import("../../../node/webui/frontend/src/views/settings/GeneralSettings.vue");
const connectionSettings = () => import("../../../node/webui/frontend/src/views/settings/ConnectionSettings.vue");
const mcpSettings = () => import("../../../node/webui/frontend/src/views/settings/McpSettings.vue");
const linuxChannelsSettings = () => import("../../../node/webui/frontend/src/views/settings/LinuxChannelsSettings.vue");
const agentsSettings = () => import("../../../node/webui/frontend/src/views/settings/AgentsSettings.vue");
const agentDetailSettings = () => import("../../../node/webui/frontend/src/views/settings/AgentDetailSettings.vue");
const capabilitiesSettings = () => import("../../../node/webui/frontend/src/views/settings/CapabilitiesSettings.vue");
const contextSettings = () => import("../../../node/webui/frontend/src/views/settings/ContextSettings.vue");
const skillsSettings = () => import("../../../node/webui/frontend/src/views/settings/SkillsSettings.vue");
const triggersSettings = () => import("../../../node/webui/frontend/src/views/settings/TriggersSettings.vue");
const securitySettings = () => import("../../../node/webui/frontend/src/views/settings/SecuritySettings.vue");
const aboutSettings = () => import("../../../node/webui/frontend/src/views/settings/AboutSettings.vue");
const feedbackSettings = () => import("../../../node/webui/frontend/src/views/settings/FeedbackSettings.vue");
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
