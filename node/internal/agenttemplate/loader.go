// Package agenttemplate 加载内置与用户自定义 Agent 模板。
package agenttemplate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Template 为 Agent 创建蓝图。
type Template struct {
	ID          string         `yaml:"id" json:"id"`
	DisplayName string         `yaml:"display_name" json:"display_name"`
	Description string         `yaml:"description" json:"description"`
	Version     int            `yaml:"version" json:"version"`
	Defaults    map[string]any `yaml:"defaults" json:"defaults,omitempty"`
	Sandbox     SandboxConfig  `yaml:"sandbox" json:"sandbox"`
}

// SandboxConfig 沙箱选项（未启用时 backend=process；启用时为 docker）。
// remote_endpoint / remote_api_key 仅兼容旧模板 YAML，创建时会被忽略或升为 docker。
type SandboxConfig struct {
	Enabled           bool   `yaml:"enabled" json:"enabled"`
	Backend           string `yaml:"backend" json:"backend"`
	WorkspaceSubdir   string `yaml:"workspace_subdir,omitempty" json:"workspace_subdir,omitempty"`
	FSRootIsolation   bool   `yaml:"fs_root_isolation" json:"fs_root_isolation"`
	AllowBash         bool   `yaml:"allow_bash" json:"allow_bash"`
	AllowNetworkTools bool   `yaml:"allow_network_tools" json:"allow_network_tools"`
	Image             string `yaml:"image,omitempty" json:"image,omitempty"`
	Network           string `yaml:"network,omitempty" json:"network,omitempty"`
	Memory            string `yaml:"memory,omitempty" json:"memory,omitempty"`
	CPUs              string `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	RemoteEndpoint    string `yaml:"remote_endpoint,omitempty" json:"remote_endpoint,omitempty"`
	RemoteAPIKey      string `yaml:"remote_api_key,omitempty" json:"remote_api_key,omitempty"`
}

// Loader 从内置目录与用户覆盖目录加载模板。
type Loader struct {
	builtinDir string
	userDir    string
}

// NewLoader 构造加载器；userDir 可为空。
func NewLoader(builtinDir, userDir string) *Loader {
	return &Loader{
		builtinDir: strings.TrimSpace(builtinDir),
		userDir:    strings.TrimSpace(userDir),
	}
}

// List 返回合并后的模板（用户覆盖同 id 的内置）。
func (l *Loader) List() ([]Template, error) {
	byID := map[string]Template{}
	if l.builtinDir != "" {
		if err := loadDirInto(l.builtinDir, byID); err != nil {
			return nil, err
		}
	}
	if l.userDir != "" {
		if err := loadDirInto(l.userDir, byID); err != nil {
			return nil, err
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Template, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, nil
}

// Get 返回指定模板；不存在时返回 error。
func (l *Loader) Get(id string) (Template, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Template{}, fmt.Errorf("template id is required")
	}
	list, err := l.List()
	if err != nil {
		return Template{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return Template{}, fmt.Errorf("template %q not found", id)
}

func loadDirInto(dir string, dst map[string]Template) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read templates dir %q: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") && !strings.HasSuffix(strings.ToLower(name), ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read template %q: %w", path, err)
		}
		var t Template
		if err := yaml.Unmarshal(raw, &t); err != nil {
			return fmt.Errorf("parse template %q: %w", path, err)
		}
		t.ID = strings.TrimSpace(t.ID)
		if t.ID == "" {
			t.ID = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
		}
		if strings.TrimSpace(t.Sandbox.Backend) == "" {
			t.Sandbox.Backend = "process"
		}
		if t.Version <= 0 {
			t.Version = 1
		}
		dst[t.ID] = t
	}
	return nil
}
