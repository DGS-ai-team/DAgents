package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvNodeID = "NODE_ID"
	// EnvAgentID 为旧环境变量名；ResolveNodeID 在 NODE_ID 为空时仍可读（兼容）。
	EnvAgentID = "AGENT_ID"
)

// NodeIDFilePath 返回 node_id 持久化文件路径（`<runtime>/node/node_id`）。
func (c *Config) NodeIDFilePath() string {
	return filepath.Join(c.RuntimeDir(), "node", "node_id")
}

// legacyAgentIDFilePath 为旧路径（`<runtime>/agent/agent_id`），解析时可读一次并迁移。
func (c *Config) legacyAgentIDFilePath() string {
	return filepath.Join(c.RuntimeDir(), "agent", "agent_id")
}

// ResolveNodeID 解析 node_id 并写入持久化文件。
//
// 逻辑：
// 1. 在 ApplyDefaults 之后调用，路径依赖 RuntimeDir；
// 2. NODE_ID 或 AGENT_ID 环境变量非空时作为权威值并写回新文件；
// 3. 否则读 `<runtime>/node/node_id`；
// 4. 否则读旧 `<runtime>/agent/agent_id` 并迁移到新路径；
// 5. 否则使用 YAML 中的 node_id，仍为空则生成 UUID。
//
// 副作用：可能创建 `.runtime/node/` 并写入 node_id 文件；修改 c.NodeID。
func (c *Config) ResolveNodeID() error {
	path := c.NodeIDFilePath()

	if envID := strings.TrimSpace(os.Getenv(EnvNodeID)); envID != "" {
		return c.persistNodeID(path, envID)
	}
	if envID := strings.TrimSpace(os.Getenv(EnvAgentID)); envID != "" {
		return c.persistNodeID(path, envID)
	}

	id, found, err := readIDFile(path)
	if err != nil {
		return err
	}
	if found {
		c.NodeID = id
		return nil
	}

	legacyPath := c.legacyAgentIDFilePath()
	id, found, err = readIDFile(legacyPath)
	if err != nil {
		return err
	}
	if found {
		return c.persistNodeID(path, id)
	}

	seed := strings.TrimSpace(c.NodeID)
	if seed == "" {
		generated, genErr := generateNodeID()
		if genErr != nil {
			return fmt.Errorf("generate node_id: %w", genErr)
		}
		seed = generated
	}
	return c.persistNodeID(path, seed)
}

func readIDFile(path string) (string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read id file %q: %w", path, err)
	}
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return "", false, nil
	}
	return id, true, nil
}

func (c *Config) persistNodeID(path, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("node_id cannot be empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create node_id dir %q: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(id), 0o644); err != nil {
		return fmt.Errorf("write node_id file %q: %w", path, err)
	}
	c.NodeID = id
	return nil
}

func generateNodeID() (string, error) {
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
