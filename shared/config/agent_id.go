package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EnvAgentID = "AGENT_ID"

// AgentIDFilePath 返回 agent_id 持久化文件路径（`<runtime>/agent/agent_id`）。
func (c *Config) AgentIDFilePath() string {
	return filepath.Join(c.RuntimeDir(), "agent", "agent_id")
}

// ResolveAgentID 解析 agent_id 并写入持久化文件。

// 逻辑：
// 1. 在 ApplyDefaults 之后调用，路径依赖 RuntimeDir；
// 2. 环境变量 AGENT_ID 非空时作为权威值并写回文件；
// 3. 否则文件存在且非空时以文件为准；
// 4. 否则使用 YAML 中的 agent_id，仍为空则生成 UUID 并写文件。
//
// 关键分支：文件为运行时权威来源；YAML 仅在文件缺失时作为种子。
//
// 副作用：可能创建 `.runtime/agent/` 并写入 agent_id 文件；修改 c.AgentID。
func (c *Config) ResolveAgentID() error {
	path := c.AgentIDFilePath()

	if envID := strings.TrimSpace(os.Getenv(EnvAgentID)); envID != "" {
		return c.persistAgentID(path, envID)
	}

	id, found, err := readAgentIDFile(path)
	if err != nil {
		return err
	}
	if found {
		c.AgentID = id
		return nil
	}

	seed := strings.TrimSpace(c.AgentID)
	if seed == "" {
		generated, genErr := generateAgentID()
		if genErr != nil {
			return fmt.Errorf("generate agent_id: %w", genErr)
		}
		seed = generated
	}
	return c.persistAgentID(path, seed)
}

func readAgentIDFile(path string) (string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read agent_id file %q: %w", path, err)
	}
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return "", false, nil
	}
	return id, true, nil
}

func (c *Config) persistAgentID(path, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("agent_id cannot be empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create agent_id dir %q: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(id), 0o644); err != nil {
		return fmt.Errorf("write agent_id file %q: %w", path, err)
	}
	c.AgentID = id
	return nil
}

func generateAgentID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	), nil
}
