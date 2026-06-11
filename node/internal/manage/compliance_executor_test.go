package manage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

const sampleComplianceCustom = `# 合规助手工作指令

你是公司 **内控与数据合规** 专职 Agent。收到其它 Agent 的【合规咨询】Task 时，仅依据下列 **内部规章制度** 给出结论：

- 允许：APPROVED | rule=R-xxxx | …
- 禁止：DENIED | rule=R-xxxx | …

## 内部规章制度（节选）

### R-PII-01 客户个人信息出境
未经法务与 DPO 双签批准备案，**禁止**向境外或未签约 SaaS 传输可识别客户的 PII。

### R-CHG-01 生产变更
生产环境发布、配置变更、数据导出必须关联已批准的变更单（CHG-20xx-xxxx）。

### R-ANON-01 脱敏统计
仅含聚合/脱敏且经 DPO 白名单 vendor 的统计指标，可在 **已完成 CHG 审批** 后出境。
`

func writeComplianceFixtures(t *testing.T) (*config.Config, *session.Manager) {
	t.Helper()
	dir := t.TempDir()
	promptDir := filepath.Join(dir, "prompt_context")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "custom.md"), []byte(sampleComplianceCustom), 0o644); err != nil {
		t.Fatal(err)
	}
	cardPath := filepath.Join(dir, "agent-card.json")
	if err := os.WriteFile(cardPath, []byte(`{"metadata":{"role":"compliance"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		AgentID: "compliance-a",
		FSRoot:  dir,
		Manage: config.ManageConfig{
			Enabled: true,
			Registration: config.ManageRegistrationConfig{
				AgentCardPath: cardPath,
			},
		},
	}
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mgr := session.NewManager(cfg.AgentID, hub, &llm.MockClient{
		FixedReply: "APPROVED | rule=R-ANON-01 | mock compliance reply",
	}, reg, pol, nil, session.TurnOptions{RuntimeDir: dir, SkillsEnabled: false}, logx.Discard())
	t.Cleanup(mgr.Stop)
	return cfg, mgr
}

func TestComplianceExecutor_llmTurnReply(t *testing.T) {
	cfg, mgr := writeComplianceFixtures(t)
	var replyBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/reply") {
			_ = json.NewDecoder(r.Body).Decode(&replyBody)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "t1", "status": "completed"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()
	cfg.Manage.URL = srv.URL

	ex := NewComplianceExecutor(cfg, mgr, nil)
	content := "【合规咨询】拟将脱敏后的日活统计发送至已备案 vendor，变更单 CHG-2026-0142，是否可执行？"
	if err := ex.HandleTask(context.Background(), InboxTask{TaskID: "t1", FromAgentID: "ops-b", Content: content}); err != nil {
		t.Fatal(err)
	}
	if replyBody["status"] != "completed" {
		t.Fatalf("status=%q", replyBody["status"])
	}
	if !strings.Contains(replyBody["result_text"], "APPROVED") || !strings.Contains(replyBody["result_text"], "R-ANON-01") {
		t.Fatalf("result=%q", replyBody["result_text"])
	}
}

func TestResolveInboxHandler_complianceRole(t *testing.T) {
	cfg, mgr := writeComplianceFixtures(t)
	handler := ResolveInboxHandler(cfg, mgr, nil)
	if handler == nil {
		t.Fatal("expected compliance handler")
	}
}
