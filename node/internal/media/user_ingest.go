package media

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// IngestDataURL 将 data:image/* URL 落盘并注册 artifact（F-M5）。
func IngestDataURL(reg *Registry, dataURL string) (*Artifact, error) {
	if reg == nil {
		return nil, ErrInvalidImage
	}
	mime, payload, err := decodeDataImageURL(dataURL)
	if err != nil {
		return nil, err
	}
	ext := extForMIME(mime)
	if ext == "" {
		return nil, ErrInvalidImage
	}
	id, err := newMediaID()
	if err != nil {
		return nil, err
	}
	rel := UserUploadRelPath(reg.sessionID, id, ext)
	abs, err := ResolveUnderRoot(reg.fsRoot, rel)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}
	if err := os.WriteFile(abs, payload, 0o644); err != nil {
		return nil, fmt.Errorf("write media file: %w", err)
	}
	return reg.registerExistingFile(id, RegisterOpts{
		RelPath: rel,
		Source:  "user_upload",
		Label:   "user_upload",
	})
}

// PersistUserMessageImages 将 user 消息中的 data: URL 替换为 dagents-media 引用。
func PersistUserMessageImages(reg *Registry, msg llm.Message) (llm.Message, error) {
	if reg == nil || !llm.MessageHasImages(msg) {
		return msg, nil
	}
	out := llm.CloneMessage(msg)
	for i, part := range out.ContentParts {
		if part.Type != "image_url" || part.ImageURL == nil {
			continue
		}
		raw := strings.TrimSpace(part.ImageURL.URL)
		if raw == "" || IsRefURL(raw) {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(raw), "data:") {
			continue
		}
		art, err := IngestDataURL(reg, raw)
		if err != nil {
			return llm.Message{}, err
		}
		detail := part.ImageURL.Detail
		out.ContentParts[i].ImageURL = &llm.ImageURLPart{
			URL:    RefURL(art.ID),
			Detail: detail,
		}
	}
	return out, nil
}

// ExpandMessagesForLLM 将 history 中的 dagents-media 引用展开为 data: URL（仅 API 调用副本）。
func ExpandMessagesForLLM(messages []llm.Message, reg *Registry) []llm.Message {
	if reg == nil || len(messages) == 0 {
		return messages
	}
	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		expanded, err := expandMessageForLLM(msg, reg)
		if err != nil {
			out[i] = msg
			continue
		}
		out[i] = expanded
	}
	return out
}

func expandMessageForLLM(msg llm.Message, reg *Registry) (llm.Message, error) {
	if !llm.MessageHasImages(msg) {
		return msg, nil
	}
	out := llm.CloneMessage(msg)
	for i, part := range out.ContentParts {
		if part.Type != "image_url" || part.ImageURL == nil {
			continue
		}
		raw := strings.TrimSpace(part.ImageURL.URL)
		id, ok := ParseRefURL(raw)
		if !ok {
			continue
		}
		dataURL, err := reg.DataURLForMediaID(id)
		if err != nil {
			return llm.Message{}, err
		}
		detail := part.ImageURL.Detail
		out.ContentParts[i].ImageURL = &llm.ImageURLPart{
			URL:    dataURL,
			Detail: detail,
		}
	}
	return out, nil
}

// DataURLForMediaID 读取已注册 media 并编码为 data URL。
func (r *Registry) DataURLForMediaID(mediaID string) (string, error) {
	art, err := r.EnsureArtifact(mediaID)
	if err != nil {
		return "", err
	}
	_, abs, err := r.OpenFile(art.ID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if len(data) > llm.MaxImageBytes {
		return "", fmt.Errorf("image too large (max %d bytes)", llm.MaxImageBytes)
	}
	return "data:" + art.MIME + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// EnsureArtifact 按 id 查找 artifact；冷启动时尝试从落盘路径恢复注册。
func (r *Registry) EnsureArtifact(mediaID string) (*Artifact, error) {
	if art, ok := r.Get(mediaID); ok {
		return art, nil
	}
	return r.registerUserUploadFromDisk(mediaID)
}

func (r *Registry) registerUserUploadFromDisk(mediaID string) (*Artifact, error) {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		rel := UserUploadRelPath(r.sessionID, mediaID, ext)
		if art, err := r.registerExistingFile(mediaID, RegisterOpts{
			RelPath: rel,
			Source:  "user_upload",
			Label:   "user_upload",
		}); err == nil {
			return art, nil
		}
	}
	return nil, ErrNotFound
}

func (r *Registry) registerExistingFile(id string, opts RegisterOpts) (*Artifact, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidImage
	}
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
	art := &Artifact{
		ID:        id,
		AgentID:   r.sessionID,
		Kind:      kindImage,
		MIME:      mime,
		Source:    strings.TrimSpace(opts.Source),
		ToolCallID: strings.TrimSpace(opts.ToolCallID),
		RelPath:   filepath.ToSlash(rel),
		Label:     strings.TrimSpace(opts.Label),
		Caption:   strings.TrimSpace(opts.Caption),
		Bytes:     info.Size(),
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	r.byID[id] = art
	r.mu.Unlock()
	return art, nil
}

func decodeDataImageURL(raw string) (mime string, payload []byte, err error) {
	raw = strings.TrimSpace(raw)
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return "", nil, ErrInvalidImage
	}
	meta := strings.ToLower(raw[:comma])
	if !strings.HasPrefix(meta, "data:image/") {
		return "", nil, ErrInvalidImage
	}
	switch {
	case strings.Contains(meta, "image/jpeg"):
		mime = "image/jpeg"
	case strings.Contains(meta, "image/png"):
		mime = "image/png"
	case strings.Contains(meta, "image/gif"):
		mime = "image/gif"
	case strings.Contains(meta, "image/webp"):
		mime = "image/webp"
	default:
		return "", nil, ErrInvalidImage
	}
	payload, err = base64.StdEncoding.DecodeString(raw[comma+1:])
	if err != nil {
		return "", nil, ErrInvalidImage
	}
	if len(payload) == 0 || len(payload) > MaxBytes {
		return "", nil, ErrInvalidImage
	}
	return mime, payload, nil
}

func extForMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

// UserMediaFromMessage 解析 user 消息中的 media 展示条目（hydrate / UI）。
func UserMediaFromMessage(msg llm.Message, reg *Registry) (images []string, mediaItems []map[string]any) {
	for _, part := range msg.ContentParts {
		if part.Type != "image_url" || part.ImageURL == nil {
			continue
		}
		raw := strings.TrimSpace(part.ImageURL.URL)
		if raw == "" {
			continue
		}
		if id, ok := ParseRefURL(raw); ok && reg != nil {
			if art, err := reg.EnsureArtifact(id); err == nil {
				item := ArtifactSSEMap(art)
				mediaItems = append(mediaItems, item)
				images = append(images, art.PublicURL())
				continue
			}
		}
		if id, ok := ParsePublicMediaURL(raw); ok && reg != nil {
			if art, err := reg.EnsureArtifact(id); err == nil {
				item := ArtifactSSEMap(art)
				mediaItems = append(mediaItems, item)
				images = append(images, art.PublicURL())
				continue
			}
		}
		images = append(images, raw)
	}
	return images, mediaItems
}

// RehydrateUserMediaFromMessages 为 hydrate 重建 user 消息 media 索引。
func RehydrateUserMediaFromMessages(reg *Registry, messages []llm.Message) map[int][]map[string]any {
	if reg == nil || len(messages) == 0 {
		return nil
	}
	out := make(map[int][]map[string]any)
	for i, msg := range messages {
		if strings.TrimSpace(msg.Role) != "user" {
			continue
		}
		_, items := UserMediaFromMessage(msg, reg)
		if len(items) > 0 {
			out[i] = items
		}
	}
	return out
}
