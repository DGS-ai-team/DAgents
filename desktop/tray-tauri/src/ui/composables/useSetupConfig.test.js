import { describe, expect, it, vi } from "vitest";

vi.mock("../api/node.js", () => ({
  getSetupConfig: vi.fn(),
  patchSetupConfig: vi.fn(),
}));

import { getSetupConfig, patchSetupConfig } from "../api/node.js";
import { useSetupConfig } from "./useSetupConfig.js";

describe("useSetupConfig Node token handling", () => {
  it("keeps credentials write-only in the form and sends explicit blank-preserve patches", async () => {
    const serverView = {
      config_writable: true,
      node: { listen_host: "127.0.0.1", listen_port: 18765 },
      manage: { enabled: true, has_node_token: true },
      llm: { profiles: [] },
    };
    getSetupConfig.mockResolvedValue(serverView);
    patchSetupConfig.mockResolvedValue(serverView);

    const state = useSetupConfig();
    await state.load();

    expect(state.form.manage.has_node_token).toBe(true);
    expect(state.form.manage.node_token).toBe("");
    expect(state.form.manage.clear_node_token).toBe(false);
    expect(state.form.node).toEqual({ listen_host: "127.0.0.1", listen_port: 18765 });

    await state.save({ manage: { node_token: "" } });
    expect(patchSetupConfig).toHaveBeenCalledWith({ manage: { node_token: "" } });
  });
});
