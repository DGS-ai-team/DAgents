import { createRouter, createWebHistory } from "vue-router";
import ChatLayout from "../layouts/ChatLayout.vue";
import ChatView from "../views/ChatView.vue";
import * as api from "../api/node.js";

const SettingsLayout = () => import("../layouts/SettingsLayout.vue");
const WorkgroupView = () => import("../views/WorkgroupView.vue");
const GeneralSettings = () => import("../views/settings/GeneralSettings.vue");
const SkillsSettings = () => import("../views/settings/SkillsSettings.vue");
const TriggersSettings = () => import("../views/settings/TriggersSettings.vue");
const SecuritySettings = () => import("../views/settings/SecuritySettings.vue");
const AboutSettings = () => import("../views/settings/AboutSettings.vue");
const ContextSettings = () => import("../views/settings/ContextSettings.vue");
const ConnectionSettings = () => import("../views/settings/ConnectionSettings.vue");
const McpSettings = () => import("../views/settings/McpSettings.vue");
const LinuxChannelsSettings = () => import("../views/settings/LinuxChannelsSettings.vue");
const CapabilitiesSettings = () => import("../views/settings/CapabilitiesSettings.vue");
const AgentsSettings = () => import("../views/settings/AgentsSettings.vue");
const AgentDetailSettings = () => import("../views/settings/AgentDetailSettings.vue");

async function requireWorkgroupEnabled() {
  try {
    const bootstrap = await api.getUIBootstrap();
    if (bootstrap?.info?.workgroup_enabled) return true;
    return {
      name: "agents",
      query: { notice: "workgroup-disabled" },
      replace: true,
    };
  } catch {
    // Let the Workgroup page surface transient connection failures. The guard
    // only redirects when the Node explicitly reports that the feature is off.
    return true;
  }
}

const router = createRouter({
  history: createWebHistory("/ui/"),
  routes: [
    { path: "/", redirect: "/agents" },
    {
      path: "/agents/:agentId?",
      component: ChatLayout,
      children: [{ path: "", name: "agents", component: ChatView }],
    },
    {
      path: "/workgroups/:workgroupId?",
      component: ChatLayout,
      beforeEnter: requireWorkgroupEnabled,
      children: [{ path: "", name: "workgroups", component: WorkgroupView }],
    },
    {
      path: "/settings",
      component: SettingsLayout,
      redirect: "/settings/general",
      children: [
        { path: "general", name: "settings-general", component: GeneralSettings },
        { path: "connection", name: "settings-connection", component: ConnectionSettings },
        { path: "mcp", name: "settings-mcp", component: McpSettings },
        { path: "linux-channels", name: "settings-linux-channels", component: LinuxChannelsSettings },
        { path: "agents", name: "settings-agents", component: AgentsSettings },
        { path: "agents/:agentId", name: "settings-agent-detail", component: AgentDetailSettings },
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
