package session

import (
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestCreateWithOptions_perAgentWorkspaceRoot(t *testing.T) {
	nodeRoot := t.TempDir()
	agentRoot := filepath.Join(t.TempDir(), "agent-data")
	baseReg, err := tools.NewRegistry(nodeRoot, 30)
	if err != nil {
		t.Fatal(err)
	}
	agentReg, err := tools.NewRegistry(agentRoot, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	hub := stream.NewHub(32, logx.Discard())
	mgr := NewManager("node-1", hub, &llm.MockClient{}, baseReg, pol, nil, TurnOptions{
		WorkspaceRoot: nodeRoot,
		MaxToolLoops: 4,
	}, logx.Discard())
	defer mgr.Stop()

	sess, created, err := mgr.CreateWithOptions("agt-sandbox-1", TurnOptions{
		WorkspaceRoot: agentRoot,
		MaxToolLoops: 4,
	}, agentReg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !created || sess.ID != "agt-sandbox-1" {
		t.Fatalf("sess=%+v created=%v", sess, created)
	}
	got, ok := mgr.SessionWorkspaceRoot("agt-sandbox-1")
	if !ok || got != agentRoot {
		t.Fatalf("workspaceRoot=%q ok=%v want %q", got, ok, agentRoot)
	}
}
