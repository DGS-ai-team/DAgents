package media

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound     = errors.New("media not found")
	ErrInvalidImage = errors.New("invalid image path or type")
)

const kindImage = "image"

// Artifact 为 Session 内可展示的媒体引用（F-M1）。
type Artifact struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	Kind       string    `json:"kind"`
	MIME       string    `json:"mime"`
	Source     string    `json:"source"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	RelPath    string    `json:"rel_path"`
	AbsPath    string    `json:"-"` // fs_root 外绝对路径；非空时 OpenFile 直接读取
	Label      string    `json:"label,omitempty"`
	Caption    string    `json:"caption,omitempty"`
	Bytes      int64     `json:"bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

// PublicURL 返回 Client 可用的相对 API 路径。
func (a Artifact) PublicURL() string {
	return fmt.Sprintf("/v1/sessions/%s/media/%s", a.SessionID, a.ID)
}

// ThumbnailURL 返回带 thumbnail=1 的相对 API 路径（F-M6）。
func (a Artifact) ThumbnailURL() string {
	return a.PublicURL() + "?thumbnail=1"
}

// RegisterOpts 注册媒体时的元数据。
type RegisterOpts struct {
	Path       string // 相对 fs_root 或绝对路径
	RelPath    string // Path 别名（兼容旧调用）
	Source     string
	ToolCallID string
	Label      string
	Caption    string
}

// Registry 维护单 session 的 media 索引（内存，F-M1）。
type Registry struct {
	sessionID string
	fsRoot    string
	mu        sync.RWMutex
	byID      map[string]*Artifact
}

// NewRegistry 创建 session 绑定的媒体注册表。
func NewRegistry(sessionID, fsRoot string) (*Registry, error) {
	root, err := ResolveFSRoot(fsRoot)
	if err != nil {
		return nil, err
	}
	return &Registry{
		sessionID: strings.TrimSpace(sessionID),
		fsRoot:    root,
		byID:      make(map[string]*Artifact),
	}, nil
}

// RegisterFromRelPath 引用已有图片文件（相对 fs_root 或绝对路径）。
func (r *Registry) RegisterFromRelPath(opts RegisterOpts) (*Artifact, error) {
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path = opts.RelPath
	}
	return r.RegisterFromPath(opts)
}

// RegisterFromPath 注册可读图片；相对路径在 fs_root 内解析，绝对路径可直接引用。
func (r *Registry) RegisterFromPath(opts RegisterOpts) (*Artifact, error) {
	raw := strings.TrimSpace(opts.Path)
	if raw == "" {
		raw = strings.TrimSpace(opts.RelPath)
	}
	if raw == "" {
		return nil, ErrInvalidImage
	}
	mime := MIMEForPath(raw)
	if mime == "" {
		return nil, ErrInvalidImage
	}
	abs, external, err := ResolveImagePath(r.fsRoot, raw)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrInvalidImage
		}
		return nil, err
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > MaxBytes {
		return nil, ErrInvalidImage
	}
	id, err := newMediaID()
	if err != nil {
		return nil, err
	}
	art := &Artifact{
		ID:         id,
		SessionID:  r.sessionID,
		Kind:       kindImage,
		MIME:       mime,
		Source:     strings.TrimSpace(opts.Source),
		ToolCallID: strings.TrimSpace(opts.ToolCallID),
		Label:      strings.TrimSpace(opts.Label),
		Caption:    strings.TrimSpace(opts.Caption),
		Bytes:      info.Size(),
		CreatedAt:  time.Now().UTC(),
	}
	if external {
		art.AbsPath = abs
		art.RelPath = filepath.ToSlash(abs)
	} else {
		rel, err := filepath.Rel(r.fsRoot, abs)
		if err != nil {
			return nil, err
		}
		art.RelPath = filepath.ToSlash(rel)
	}
	r.mu.Lock()
	r.byID[id] = art
	r.mu.Unlock()
	return art, nil
}

// Get 按 id 查找 artifact。
func (r *Registry) Get(id string) (*Artifact, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	art, ok := r.byID[strings.TrimSpace(id)]
	if !ok || art == nil {
		return nil, false
	}
	copy := *art
	return &copy, true
}

// OpenFile 打开 artifact 对应文件的绝对路径。
func (r *Registry) OpenFile(id string) (*Artifact, string, error) {
	art, ok := r.Get(id)
	if !ok {
		return nil, "", ErrNotFound
	}
	abs, err := r.artifactAbsPath(art)
	if err != nil {
		return nil, "", err
	}
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	return art, abs, nil
}

func (r *Registry) artifactAbsPath(art *Artifact) (string, error) {
	if art == nil {
		return "", ErrNotFound
	}
	if p := strings.TrimSpace(art.AbsPath); p != "" {
		return filepath.Abs(p)
	}
	return ResolveUnderRoot(r.fsRoot, art.RelPath)
}

func newMediaID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate media id: %w", err)
	}
	return "med_" + hex.EncodeToString(b[:]), nil
}
