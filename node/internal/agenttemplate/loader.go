// Package agenttemplate 加载内置与用户自定义 Agent 模板。
package agenttemplate

import (
	"fmt"
	"io/fs"
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
}

// Loader 从嵌入内置、可选磁盘内置目录与用户覆盖目录加载模板。
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

// List 返回合并后的模板（优先级：用户目录 > 磁盘内置目录 > 嵌入内置）。
func (l *Loader) List() ([]Template, error) {
	byID := map[string]Template{}
	if err := loadFSInto(builtinTemplatesFS, "builtin", byID); err != nil {
		return nil, err
	}
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
		if !isYAMLName(name) {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read template %q: %w", path, err)
		}
		t, err := parseTemplateYAML(name, raw)
		if err != nil {
			return err
		}
		dst[t.ID] = t
	}
	return nil
}

func loadFSInto(fsys fs.FS, root string, dst map[string]Template) error {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read embedded templates %q: %w", root, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isYAMLName(name) {
			continue
		}
		path := root + "/" + name
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read embedded template %q: %w", path, err)
		}
		t, err := parseTemplateYAML(name, raw)
		if err != nil {
			return err
		}
		dst[t.ID] = t
	}
	return nil
}

func isYAMLName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

func parseTemplateYAML(name string, raw []byte) (Template, error) {
	var t Template
	if err := yaml.Unmarshal(raw, &t); err != nil {
		return Template{}, fmt.Errorf("parse template %q: %w", name, err)
	}
	t.ID = strings.TrimSpace(t.ID)
	if t.ID == "" {
		t.ID = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
	}
	if t.Version <= 0 {
		t.Version = 1
	}
	return t, nil
}
