import {
  applyAgentUpdate,
  getAgentUpdate,
  getPlatformCapabilities,
  getPlatformClipboardFiles,
  pickPlatformDirectory,
  reportPlatformUIFocus,
} from "./node.js";

/**
 * Node 平台能力适配层。
 *
 * 这里不包含桌面端地址、CORS 或 Shell fallback；WebUI 始终只和当前
 * Node 的同源 API 通信，由 Node 决定宿主能力是否可用。
 */
export {
  getPlatformCapabilities,
  pickPlatformDirectory,
  getPlatformClipboardFiles,
  reportPlatformUIFocus,
  applyAgentUpdate,
};

/** 返回 Node 权威更新状态及当前是否可执行安装。 */
export async function getUpdateStatus() {
  const [data, capabilities] = await Promise.all([
    getAgentUpdate(),
    getPlatformCapabilities(),
  ]);
  return {
    ...data,
    apply_available: capabilities?.update_apply === true,
  };
}
