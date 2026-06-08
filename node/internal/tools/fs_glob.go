package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	defaultGlobMaxResults = 100
	defaultGrepFilesGlob  = "**/*"
)

var defaultWalkSkipDirNames = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
}

type globCollectOptions struct {
	includeDirs bool
	offset      int
	maxResults  int
}

func normalizeGlobPattern(pattern string) (string, error) {
	pat := strings.TrimSpace(pattern)
	if pat == "" {
		return "", fmt.Errorf("glob_pattern 不能为空")
	}
	return filepath.ToSlash(pat), nil
}

func shouldSkipWalkDir(name string) bool {
	_, ok := defaultWalkSkipDirNames[name]
	return ok
}

// collectGlobMatches 在 dirAbs 下按 glob（相对 directory 根，支持 **）收集路径，返回相对 fsRoot 的路径。
func (r *Registry) collectGlobMatches(dirRel, globPattern string, opt globCollectOptions) ([]string, int, error) {
	dirRel = strings.TrimSpace(dirRel)
	if dirRel == "" {
		dirRel = "."
	}
	dirAbs, err := r.resolvePath(dirRel)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(dirAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("目录不存在：%q", dirRel)
		}
		return nil, 0, err
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("directory 必须是目录：%q", dirRel)
	}

	pat, err := normalizeGlobPattern(globPattern)
	if err != nil {
		return nil, 0, err
	}

	offset := opt.offset
	if offset < 0 {
		offset = 0
	}
	maxResults := opt.maxResults
	if maxResults <= 0 {
		maxResults = defaultGlobMaxResults
	}

	var all []string
	err = filepath.Walk(dirAbs, func(walkPath string, ent os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ent.IsDir() {
			if walkPath != dirAbs && shouldSkipWalkDir(ent.Name()) {
				return filepath.SkipDir
			}
			if !opt.includeDirs {
				return nil
			}
		}

		relFromDir, err := filepath.Rel(dirAbs, walkPath)
		if err != nil {
			return err
		}
		if relFromDir == "." {
			return nil
		}
		relSlash := filepath.ToSlash(relFromDir)
		matched, err := doublestar.Match(pat, relSlash)
		if err != nil {
			return fmt.Errorf("glob_pattern 无效: %w", err)
		}
		if !matched {
			return nil
		}
		if ent.IsDir() && !opt.includeDirs {
			return nil
		}

		relFromRoot, err := filepath.Rel(r.fsRoot, walkPath)
		if err != nil {
			return err
		}
		all = append(all, filepath.ToSlash(relFromRoot))
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	sort.Strings(all)
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + maxResults
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

// collectGlobFilePaths 在 directory 下收集匹配 glob 的普通文件路径（相对 fs_root），受 maxFiles 限制。
func (r *Registry) collectGlobFilePaths(dirRel, globPattern string, maxFiles int) ([]string, int, error) {
	dirRel = strings.TrimSpace(dirRel)
	if dirRel == "" {
		dirRel = "."
	}
	dirAbs, err := r.resolvePath(dirRel)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(dirAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("目录不存在：%q", dirRel)
		}
		return nil, 0, err
	}
	if !info.IsDir() {
		return nil, 0, fmt.Errorf("directory 必须是目录：%q", dirRel)
	}

	pat, err := normalizeGlobPattern(globPattern)
	if err != nil {
		return nil, 0, err
	}
	if maxFiles <= 0 {
		maxFiles = 50
	}

	var files []string
	scanned := 0
	err = filepath.Walk(dirAbs, func(walkPath string, ent os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ent.IsDir() {
			if walkPath != dirAbs && shouldSkipWalkDir(ent.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		scanned++

		relFromDir, err := filepath.Rel(dirAbs, walkPath)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(relFromDir)
		matched, err := doublestar.Match(pat, relSlash)
		if err != nil {
			return fmt.Errorf("glob_pattern 无效: %w", err)
		}
		if !matched {
			return nil
		}
		if !isTextReadable(walkPath) {
			return nil
		}

		relFromRoot, err := filepath.Rel(r.fsRoot, walkPath)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relFromRoot))
		if len(files) >= maxFiles {
			return errGlobFileCapReached
		}
		return nil
	})
	if err != nil && err != errGlobFileCapReached {
		return nil, scanned, err
	}
	sort.Strings(files)
	return files, scanned, nil
}

var errGlobFileCapReached = fmt.Errorf("glob file cap reached")
