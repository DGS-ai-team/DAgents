package llm

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	MaxFileReferences = 8
	MaxFilePathBytes  = 4096
)

// FileReference identifies a local file explicitly selected by the user.
// It is durable message metadata, not part of the text rendered in the UI.
type FileReference struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// NormalizeFileReferences validates and canonicalizes references at the
// message boundary. The runtime still decides whether a path is readable;
// this function only prevents malformed or ambiguous message metadata.
func NormalizeFileReferences(refs []FileReference) ([]FileReference, error) {
	if len(refs) > MaxFileReferences {
		return nil, fmt.Errorf("too many file references (max %d)", MaxFileReferences)
	}
	out := make([]FileReference, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		path := normalizeFileReferencePath(ref.Path)
		if path == "" {
			return nil, fmt.Errorf("file reference path is required")
		}
		if len([]byte(path)) > MaxFilePathBytes {
			return nil, fmt.Errorf("file reference path is too long (max %d bytes)", MaxFilePathBytes)
		}
		key := fileReferenceKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			name = fileReferenceBaseName(path)
		}
		out = append(out, FileReference{Path: path, Name: name})
	}
	return out, nil
}

func normalizeFileReferencePath(raw string) string {
	path := strings.TrimSpace(raw)
	path = strings.Trim(path, "\"")
	if path == "" {
		return ""
	}
	if isWindowsFileReferencePath(path) {
		return strings.ReplaceAll(path, "/", "\\")
	}
	return filepath.Clean(path)
}

func isWindowsFileReferencePath(path string) bool {
	return len(path) >= 2 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' || strings.HasPrefix(path, `\\`)
}

func fileReferenceKey(path string) string {
	if isWindowsFileReferencePath(path) {
		return strings.ToLower(strings.ReplaceAll(path, "/", "\\"))
	}
	return path
}

func fileReferenceBaseName(path string) string {
	path = strings.TrimRight(path, "/\\")
	if index := strings.LastIndexAny(path, "/\\"); index >= 0 {
		return path[index+1:]
	}
	return path
}

// FileReferencePrompt is only used while serializing a model request. It is
// intentionally separate from Message.Content so the internal notation never
// leaks into the user's transcript bubble.
func FileReferencePrompt(refs []FileReference) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("用户明确引用的本地文件（请按需使用文件工具读取）：")
	for _, ref := range refs {
		b.WriteString("\n- ")
		b.WriteString(ref.Name)
		b.WriteString(" | ")
		b.WriteString(ref.Path)
	}
	return b.String()
}
