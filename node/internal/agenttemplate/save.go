package agenttemplate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var templateIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ValidateID 校验模板 id（小写 slug）。
func ValidateID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("template id is required")
	}
	if !templateIDPattern.MatchString(id) {
		return fmt.Errorf("template id must match %s", templateIDPattern.String())
	}
	return nil
}

// Normalize 填充缺省字段。
func (t *Template) Normalize() {
	t.ID = strings.TrimSpace(t.ID)
	t.DisplayName = strings.TrimSpace(t.DisplayName)
	t.Description = strings.TrimSpace(t.Description)
	if strings.TrimSpace(t.Sandbox.Backend) == "" {
		t.Sandbox.Backend = "process"
	}
	if strings.TrimSpace(t.Sandbox.WorkspaceSubdir) == "" {
		t.Sandbox.WorkspaceSubdir = "data"
	}
	if t.Version <= 0 {
		t.Version = 1
	}
	if t.Defaults == nil {
		t.Defaults = map[string]any{}
	}
}

// SaveUser 将模板写入用户目录（`<runtime>/agent-templates/<id>.yaml`）。
func SaveUser(userDir string, t Template) error {
	userDir = strings.TrimSpace(userDir)
	if userDir == "" {
		return fmt.Errorf("user templates dir is required")
	}
	t.Normalize()
	if err := ValidateID(t.ID); err != nil {
		return err
	}
	if t.DisplayName == "" {
		t.DisplayName = t.ID
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}
	path := filepath.Join(userDir, t.ID+".yaml")
	raw, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal template: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write template %q: %w", path, err)
	}
	return nil
}

// DeleteUser 删除用户目录中的模板；内置模板不可删。
func DeleteUser(userDir, id string) error {
	userDir = strings.TrimSpace(userDir)
	if err := ValidateID(id); err != nil {
		return err
	}
	path := filepath.Join(userDir, id+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("template %q not found in user dir", id)
		}
		return err
	}
	return nil
}

// IsUserTemplate 判断 id 是否存在于用户目录。
func IsUserTemplate(userDir, id string) bool {
	userDir = strings.TrimSpace(userDir)
	id = strings.TrimSpace(id)
	if userDir == "" || id == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(userDir, id+".yaml"))
	return err == nil
}
