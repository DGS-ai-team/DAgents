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
	Label      string    `json:"label,omitempty"`
	Caption    string    `json:"caption,omitempty"`
	Bytes      int64     `json:"bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

// PublicURL 返回 Client 可用的相对 API 路径。
func (a Artifact) PublicURL() string {
	return fmt.Sprintf("/v1/sessions/%s/media/%s", a.SessionID, a.ID)
}

// RegisterOpts 注册媒体时的元数据。
type RegisterOpts struct {
	RelPath    string
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

// RegisterFromRelPath 引用 fs_root 内已有图片文件。
func (r *Registry) RegisterFromRelPath(opts RegisterOpts) (*Artifact, error) {
	rel := strings.TrimSpace(opts.RelPath)
	if rel == "" {
		return nil, ErrInvalidImage
	}
	mime := MIMEForPath(rel)
	if mime == "" {
		return nil, ErrInvalidImage
	}
	abs, err := ResolveUnderRoot(r.fsRoot, rel)
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
		RelPath:    filepath.ToSlash(rel),
		Label:      strings.TrimSpace(opts.Label),
		Caption:    strings.TrimSpace(opts.Caption),
		Bytes:      info.Size(),
		CreatedAt:  time.Now().UTC(),
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
	abs, err := ResolveUnderRoot(r.fsRoot, art.RelPath)
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

func newMediaID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate media id: %w", err)
	}
	return "med_" + hex.EncodeToString(b[:]), nil
}
