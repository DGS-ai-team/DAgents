package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/a2aclient"
	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/sandbox"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

// Registry 注册内置工具并在 FS_ROOT 沙箱内执行。
type Registry struct {
	fsRoot              string
	bashTimeout         int
	shellOutputEncoding string
	fileEncoding        string
	bashCompress        BashCompressConfig
	compressMu          sync.Mutex
	bashCompressStats   map[string]*OutputCompressStats
	visionMu            sync.Mutex
	readImageVision     map[string]*ReadImageVisionPayload
	bgJobs              *backgroundJobRegistry
	triggerStore        *triggers.Store
	triggerSched        *triggers.Scheduler
	manageClient  *a2aclient.Client
	a2aCallerHITL a2aclient.A2ACallerHITLHandler
	agentID       string
	skillsCatalogHolder
	enabledOnly         map[string]struct{}
	multimodalEnabled   bool
	browser             *browser.Manager
	handlers            map[string]handler
	pathEncMu           sync.Mutex
	pathEncCache        map[string]pathEncodingEntry
	mediaMu             sync.Mutex
	mediaRegister       MediaRegisterFunc
	toolResultMedia     map[string][]map[string]any
	dockerSandbox       *sandbox.DockerRunner
}

// NewRegistry 创建工具表；fsRoot 为空时用当前目录。
// encodings[0]=tools.bash_output_encoding，encodings[1]=tools.file_encoding；空串表示按平台/shell 自动选择。
func NewRegistry(fsRoot string, bashTimeoutSeconds int, encodings ...string) (*Registry, error) {
	root, err := resolveFSRoot(fsRoot)
	if err != nil {
		return nil, err
	}
	if bashTimeoutSeconds <= 0 {
		bashTimeoutSeconds = 30
	}
	shellEnc := ""
	fileEnc := ""
	if len(encodings) > 0 {
		shellEnc = strings.TrimSpace(encodings[0])
	}
	if len(encodings) > 1 {
		fileEnc = strings.TrimSpace(encodings[1])
	}
	r := &Registry{
		fsRoot:              root,
		bashTimeout:         bashTimeoutSeconds,
		shellOutputEncoding: shellEnc,
		fileEncoding:        fileEnc,
		bashCompress:        DefaultBashCompressConfig(),
		bgJobs:              newBackgroundJobRegistry(),
		handlers:            make(map[string]handler),
	}
	r.registerBuiltins()
	return r, nil
}

// Definitions 返回传给 LLM 的 tools 列表。
func (r *Registry) Definitions() []ToolDef {
	base := []ToolDef{
		readFileToolDef(),
		showImageToolDef(),
	}
	if r.multimodalEnabled {
		base = append(base, readImageToolDef())
	}
	base = append(base,
		writeFileToolDef(),
		globFilesToolDef(),
		grepFileToolDef(),
		grepFilesToolDef(),
		searchReplaceToolDef(),
		bashRunToolDef(),
		backgroundJobStatusToolDef(),
		backgroundJobCancelToolDef(),
		askUserInformationToolDef(),
		rememberToolDef(),
		loadSkillsToolDef(),
		unloadSkillsToolDef(),
		clearSkillsToolDef(),
		triggerListToolDef(),
		triggerGetToolDef(),
		triggerCreateToolDef(),
		triggerUpdateToolDef(),
		triggerDeleteToolDef(),
	)
	if r.manageClient != nil {
		base = append(base, manageA2AToolDefs()...)
	}
	if r.browserToolsEnabled() {
		base = append(base, r.browserToolDefs()...)
	}
	base = append(base, childAgentToolDefs()...)
	return r.enrichDefinitions(r.filterToolDefs(base))
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
	r.handlers["show_image"] = r.execShowImage
	r.handlers["read_image"] = r.execReadImage
	r.handlers["write_file"] = r.execWriteFile
	r.handlers["glob_files"] = r.execGlobFiles
	r.handlers["grep_file"] = r.execGrepFile
	r.handlers["grep_files"] = r.execGrepFiles
	r.handlers["search_file"] = r.execSearchFile
	r.handlers["search_replace"] = r.execSearchReplace
	r.handlers["bash_run"] = r.execBashRun
	r.handlers["background_job_status"] = r.execBackgroundJobStatus
	r.handlers["background_job_cancel"] = r.execBackgroundJobCancel
	r.handlers["ask_user_information"] = func(context.Context, json.RawMessage) (string, error) {
		return "", fmt.Errorf("ask_user_information must be handled by orchestrator")
	}
	r.handlers["remember"] = func(context.Context, json.RawMessage) (string, error) {
		return "", fmt.Errorf("remember must be handled by orchestrator")
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
	r.RegisterChildAgentToolStubs()
}
