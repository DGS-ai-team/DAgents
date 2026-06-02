// Package tools 提供 Node 本地工具 registry 与执行器（N3：bash/fs）。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

// FunctionDef 为 OpenAI function tool 定义。
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolDef 为 OpenAI tools 数组项。
type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// Registry 注册内置工具并在 FS_ROOT 沙箱内执行。
type Registry struct {
	fsRoot       string
	bashTimeout  int
	bgJobs       *backgroundJobRegistry
	triggerStore *triggers.Store
	triggerSched *triggers.Scheduler
	agentID      string
	handlers     map[string]handler
}

type handler func(ctx context.Context, args json.RawMessage) (string, error)

// NewRegistry 创建工具表；fsRoot 为空时用当前目录。
func NewRegistry(fsRoot string, bashTimeoutSeconds int) (*Registry, error) {
	root, err := resolveFSRoot(fsRoot)
	if err != nil {
		return nil, err
	}
	if bashTimeoutSeconds <= 0 {
		bashTimeoutSeconds = 30
	}
	r := &Registry{
		fsRoot:      root,
		bashTimeout: bashTimeoutSeconds,
		bgJobs:      newBackgroundJobRegistry(),
		handlers:    make(map[string]handler),
	}
	r.registerBuiltins()
	return r, nil
}

// Definitions 返回传给 LLM 的 tools 列表。
func (r *Registry) Definitions() []ToolDef {
	base := []ToolDef{
		readFileToolDef(),
		writeFileToolDef(),
		searchFileToolDef(),
		searchReplaceToolDef(),
		bashRunToolDef(),
		backgroundJobStatusToolDef(),
		backgroundJobCancelToolDef(),
		askUserInformationToolDef(),
		loadSkillsToolDef(),
		unloadSkillsToolDef(),
		clearSkillsToolDef(),
		triggerListToolDef(),
		triggerGetToolDef(),
		triggerCreateToolDef(),
		triggerUpdateToolDef(),
		triggerDeleteToolDef(),
		triggerFireToolDef(),
	}
	return append(base, childAgentToolDefs()...)
}

// Execute 按名称 dispatch 工具；未知工具返回 error 文本。
func (r *Registry) Execute(ctx context.Context, name, arguments string) (string, error) {
	h, ok := r.handlers[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return h(ctx, json.RawMessage(arguments))
}

func (r *Registry) registerBuiltins() {
	r.handlers["read_file"] = r.execReadFile
	r.handlers["write_file"] = r.execWriteFile
	r.handlers["search_file"] = r.execSearchFile
	r.handlers["search_replace"] = r.execSearchReplace
	r.handlers["bash_run"] = r.execBashRun
	r.handlers["background_job_status"] = r.execBackgroundJobStatus
	r.handlers["background_job_cancel"] = r.execBackgroundJobCancel
	r.handlers["ask_user_information"] = func(context.Context, json.RawMessage) (string, error) {
		return "", fmt.Errorf("ask_user_information must be handled by orchestrator")
	}
	for _, name := range []string{"load_skills", "unload_skills", "clear_skills"} {
		n := name
		r.handlers[n] = func(context.Context, json.RawMessage) (string, error) {
			return "", fmt.Errorf("%s must be handled by orchestrator", n)
		}
	}
	r.handlers["trigger_list"] = r.execTriggerList
	r.handlers["trigger_get"] = r.execTriggerGet
	r.handlers["trigger_create"] = r.execTriggerCreate
	r.handlers["trigger_update"] = r.execTriggerUpdate
	r.handlers["trigger_delete"] = r.execTriggerDelete
	r.handlers["trigger_fire"] = r.execTriggerFire
	r.RegisterChildAgentToolStubs()
}

func resolveFSRoot(fsRoot string) (string, error) {
	root := strings.TrimSpace(fsRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("fs_root empty and getwd failed: %w", err)
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create fs_root: %w", err)
	}
	return abs, nil
}

func (r *Registry) resolvePath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path not allowed: %s", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes fs_root: %s", rel)
	}
	full := filepath.Join(r.fsRoot, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	root := r.fsRoot
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		return "", fmt.Errorf("path escapes fs_root: %s", rel)
	}
	return abs, nil
}
