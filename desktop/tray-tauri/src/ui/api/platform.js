import {
  applyAgentUpdate,
  getAgentUpdate,
  getPlatformCapabilities,
  getPlatformClipboardFiles,
  pickPlatformDirectory,
  reportPlatformUIFocus,
} from "./node.js";

/**
 * Node platform capability adapter used by the Desktop UI.
 *
 * It keeps Node-owned capabilities separate from Desktop-only host requests.
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
