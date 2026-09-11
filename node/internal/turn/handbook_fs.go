package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HandbookReader reads a filesystem handbook already bound to one Agent.
type HandbookReader interface {
	Read(context.Context) (HandbookSnapshot, error)
}

type HandbookSnapshot struct{ Root, Index, Digest, Error string }

type FileHandbookReader struct{ root string }

func NewFileHandbookReader(root string) (*FileHandbookReader, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return nil, errors.New("handbook root required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &FileHandbookReader{root: root}, nil
}

func (r *FileHandbookReader) Read(ctx context.Context) (HandbookSnapshot, error) {
	if r == nil {
		return HandbookSnapshot{}, errors.New("handbook reader unavailable")
	}
	if err := ctx.Err(); err != nil {
		return HandbookSnapshot{}, err
	}
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return HandbookSnapshot{}, err
	}
	var paths []string
	for n, entry := range entries {
		if n >= 256 {
			break
		}
		if entry.Name() == ".history" {
			continue
		}
		if entry.IsDir() {
			paths = append(paths, entry.Name()+"/")
			continue
		}
		paths = append(paths, entry.Name())
	}
	var b strings.Builder
	for _, path := range paths {
		if strings.HasSuffix(path, "/") {
			b.WriteString("### ")
			b.WriteString(path)
			b.WriteString("\n")
			continue
		}
		if path != "README.md" && path != "HANDBOOK.md" && path != "index.md" {
			b.WriteString("### ")
			b.WriteString(path)
			b.WriteString("\n")
			continue
		}
		full := filepath.Join(r.root, path)
		real, e := filepath.EvalSymlinks(full)
		if e != nil {
			return HandbookSnapshot{}, e
		}
		if !withinPath(r.root, real) {
			return HandbookSnapshot{}, errors.New("handbook symlink escapes root")
		}
		f, e := os.Open(full)
		if e != nil {
			return HandbookSnapshot{}, e
		}
		raw, e := io.ReadAll(io.LimitReader(f, 16*1024+1))
		_ = f.Close()
		if e != nil {
			return HandbookSnapshot{}, e
		}
		if len(raw) > 16*1024 {
			continue
		}
		rel := path
		if b.Len()+len(raw) > 64*1024 {
			break
		}
		b.WriteString("### ")
		b.WriteString(filepath.ToSlash(rel))
		b.WriteString("\n")
		b.Write(raw)
		b.WriteString("\n\n")
	}
	content := strings.TrimSpace(b.String())
	if content == "" {
		return HandbookSnapshot{}, nil
	}
	sum := sha256.Sum256([]byte(content))
	return HandbookSnapshot{Root: r.root, Index: content, Digest: hex.EncodeToString(sum[:])}, nil
}

func withinPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
