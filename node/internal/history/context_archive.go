package history

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// ArchiveIdleContext writes a compact, model-readable projection of canonical
// session history. baseDir is the Agent state root (.dagents/<agent>); the
// returned absolute path is suitable for a runtime bridge reference.
func ArchiveIdleContext(baseDir, sessionID, resetID string, messages []llm.Message) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	sid, rid := strings.TrimSpace(sessionID), strings.TrimSpace(resetID)
	if baseDir == "" || sid == "" || rid == "" {
		return "", fmt.Errorf("archive base directory, session ID, and reset ID are required")
	}
	data, err := json.MarshalIndent(projectArchiveMessages(messages), "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal context archive: %w", err)
	}
	data = append(data, '\n')
	day := time.Now().Format("2006-01-02")
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve context archive root: %w", err)
	}
	name := contextArchiveFilename(sid, rid)
	historyDir := filepath.Join(absBase, "history")
	// Retries can cross local midnight. Find an archive for this reset ID
	// before choosing today's directory so the first path remains canonical.
	if matches, _ := filepath.Glob(filepath.Join(historyDir, "*", name)); len(matches) > 0 {
		canonicalPath := matches[0]
		for _, existingPath := range matches {
			existing, readErr := os.ReadFile(existingPath)
			if readErr != nil {
				return "", fmt.Errorf("read existing context archive: %w", readErr)
			}
			if !bytes.Equal(existing, data) {
				return "", fmt.Errorf("context archive reset ID already exists with different content: %s", existingPath)
			}
		}
		return canonicalPath, nil
	}
	dir := filepath.Join(historyDir, day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create context archive directory: %w", err)
	}
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return path, nil
		}
		return "", fmt.Errorf("context archive reset ID already exists with different content: %s", path)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read existing context archive: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".context-archive-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create context archive temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("write context archive: %w", err)
	}
	// A hard link publishes the fully written file atomically without replacing
	// a prior archive, including when two retries race on the same reset ID.
	if err := os.Link(tmpName, path); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			return path, nil
		}
		if existing, readErr := os.ReadFile(path); readErr == nil && !bytes.Equal(existing, data) {
			return "", fmt.Errorf("context archive reset ID already exists with different content: %s", path)
		}
		return "", fmt.Errorf("publish context archive: %w", err)
	}
	return path, nil
}

func contextArchiveFilename(sessionID, resetID string) string {
	safeSession := sanitizeSessionIDForFilename(sessionID)
	if len(safeSession) > 96 {
		safeSession = safeSession[:96]
	}
	safeReset := sanitizeSessionIDForFilename(resetID)
	if len(safeReset) > 32 {
		safeReset = safeReset[:32]
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(resetID)))
	return safeSession + "-" + safeReset + "-" + fmt.Sprintf("%x", digest[:8]) + ".json"
}

type archiveMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func projectArchiveMessages(messages []llm.Message) []archiveMessage {
	toolNames := make(map[string]string)
	for _, m := range messages {
		for _, call := range m.ToolCalls {
			toolNames[call.ID] = call.Function.Name
		}
	}
	out := make([]archiveMessage, 0, len(messages))
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			continue
		}
		content := m.Content
		if len(m.ContentParts) > 0 {
			var textParts, attachmentParts []string
			for _, p := range m.ContentParts {
				switch p.Type {
				case "text":
					if strings.TrimSpace(p.Text) != "" {
						textParts = append(textParts, p.Text)
					}
				case "image_url":
					if p.ImageURL != nil {
						attachmentParts = append(attachmentParts, readableAttachment(p.ImageURL.URL))
					}
				}
			}
			if strings.TrimSpace(content) == "" {
				content = strings.Join(textParts, "\n")
			} else {
				partsText := strings.Join(textParts, "\n")
				if partsText != "" && !strings.Contains(content, partsText) {
					content += "\n" + partsText
				}
			}
			content = strings.TrimSpace(content + "\n" + strings.Join(attachmentParts, "\n"))
		}
		for _, ref := range m.FileReferences {
			content += fmt.Sprintf("\n[Attached file: %s]", readableFileReference(ref))
		}
		if len(m.ToolCalls) > 0 {
			for _, call := range m.ToolCalls {
				content += fmt.Sprintf("\n[Tool call: %s]\nArguments: %s", call.Function.Name, readableArguments(call.Function.Arguments))
			}
		}
		if role == "tool" {
			name := strings.TrimSpace(m.Name)
			if name == "" {
				name = toolNames[m.ToolCallID]
			}
			if name == "" {
				name = "tool"
			}
			content = fmt.Sprintf("[Tool result: %s]\nResult: %s", name, content)
			if m.ToolResultMetadata != nil {
				content += "\nStatus: " + m.ToolResultMetadata.Status
				if m.ToolResultMetadata.Error != nil {
					content += fmt.Sprintf("\nError: %s: %s", m.ToolResultMetadata.Error.Code, m.ToolResultMetadata.Error.Message)
				}
			}
		}
		out = append(out, archiveMessage{Role: role, Content: strings.TrimSpace(content)})
	}
	return out
}

func readableFileReference(ref llm.FileReference) string {
	if strings.TrimSpace(ref.Path) != "" {
		return ref.Path
	}
	return ref.Name
}

func readableAttachment(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		return "[image attachment omitted: inline image data]"
	}
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return "[image attachment: " + raw + "]"
	}
	return "[image attachment: " + raw + "]"
}

func readableArguments(raw string) string {
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		if pretty, err := json.MarshalIndent(value, "", "  "); err == nil {
			return string(pretty)
		}
	}
	return raw
}
