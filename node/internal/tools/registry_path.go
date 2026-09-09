package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func resolveWorkspaceRoot(workspaceRoot string) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("workspace root empty and getwd failed: %w", err)
		}
		root = wd
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create workspace root: %w", err)
	}
	return abs, nil
}

func (r *Registry) resolvePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(raw) {
		abs, err := filepath.Abs(filepath.Clean(raw))
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	// Identify the reserved namespace before Clean: handbook/../x must not
	// become an ordinary workspace path. Accept both separators so requests
	// authored for Windows are checked consistently on every host.
	if r != nil && r.handbookRoot != "" && isHandbookPath(raw) {
		rel := strings.TrimLeft(raw[len("handbook"):], "/\\")
		rel = strings.ReplaceAll(strings.ReplaceAll(rel, "\\", string(os.PathSeparator)), "/", string(os.PathSeparator))
		cleanRel := filepath.Clean(rel)
		for _, part := range strings.Split(filepath.ToSlash(cleanRel), "/") {
			if part == ".history" {
				return "", fmt.Errorf("handbook history is private: %s", raw)
			}
		}
		if cleanRel == ".." || cleanRel == "."+string(os.PathSeparator)+".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("path escapes handbook root: %s", raw)
		}
		root := filepath.Clean(r.handbookRoot)
		candidate, err := filepath.Abs(filepath.Join(root, cleanRel))
		if err != nil || !pathWithin(root, candidate) {
			return "", fmt.Errorf("path escapes handbook root: %s", raw)
		}
		return resolveRealPathWithin(root, candidate, raw)
	}
	clean := filepath.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace root: %s", raw)
	}
	full := filepath.Join(r.workspaceRoot, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	root := r.workspaceRoot
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		return "", fmt.Errorf("path escapes workspace root: %s", raw)
	}
	return abs, nil
}

func isHandbookPath(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "handbook" || (len(raw) > len("handbook") && raw[:len("handbook")] == "handbook" && (raw[len("handbook")] == '/' || raw[len("handbook")] == '\\'))
}

// resolveRealPathWithin canonicalizes the existing prefix of a path. This is
// needed for a new file below a symlink/junction: EvalSymlinks on the final
// path fails with ENOENT and would otherwise allow the write to escape.
func resolveRealPathWithin(root, candidate, raw string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve handbook root: %w", err)
	}
	rootReal, _ = filepath.Abs(rootReal)
	existing := candidate
	var suffix []string
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("path does not resolve: %s", raw)
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	realExisting, _ = filepath.Abs(realExisting)
	if !pathWithin(rootReal, realExisting) {
		return "", fmt.Errorf("path escapes handbook root through link: %s", raw)
	}
	resolved := realExisting
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return resolved, nil
}

func pathWithin(root, path string) bool {
	if runtime.GOOS == "windows" {
		root, path = strings.ToLower(root), strings.ToLower(path)
	}
	root, path = filepath.Clean(root), filepath.Clean(path)
	if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
