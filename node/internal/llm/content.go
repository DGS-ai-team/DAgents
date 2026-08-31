package llm

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

const (
	MaxUserContentParts = 16
	MaxUserImages       = 8
	MaxImageBytes       = 10 << 20 // 10MB decoded
)

// ImageURLPart 为 OpenAI 兼容 image_url 载荷。
type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// ContentPart 为 user 多模态 content 数组项（text / image_url）。
type ContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

// UserInputValid 判断 user 消息是否至少含文本或图片。
func UserInputValid(text string, parts []ContentPart) bool {
	_, _, err := NormalizeUserInput(text, parts)
	return err == nil
}

// UserInputValidWithFileReferences also accepts a message containing only
// explicitly selected local files.
func UserInputValidWithFileReferences(text string, parts []ContentPart, refs []FileReference) bool {
	_, _, _, err := NormalizeUserInputWithFileReferences(text, parts, refs)
	return err == nil
}

// UserInputHasImages 判断 user 输入是否含 image_url part。
func UserInputHasImages(_ string, parts []ContentPart) bool {
	for _, part := range parts {
		if part.Type == "image_url" && part.ImageURL != nil && strings.TrimSpace(part.ImageURL.URL) != "" {
			return true
		}
	}
	return false
}

// NormalizeUserInput 合并 content 与 content_parts，并校验多模态载荷。
func NormalizeUserInput(text string, parts []ContentPart) (string, []ContentPart, error) {
	summary, normalized, _, err := NormalizeUserInputWithFileReferences(text, parts, nil)
	return summary, normalized, err
}

// NormalizeUserInputWithFileReferences normalizes multimodal content and
// durable file metadata together at the user-message boundary.
func NormalizeUserInputWithFileReferences(text string, parts []ContentPart, refs []FileReference) (string, []ContentPart, []FileReference, error) {
	text = strings.TrimSpace(text)
	normalizedRefs, err := NormalizeFileReferences(refs)
	if err != nil {
		return "", nil, nil, err
	}
	out := make([]ContentPart, 0, len(parts)+1)
	hasTextPart := false
	imageCount := 0

	for _, part := range parts {
		switch strings.TrimSpace(part.Type) {
		case "text":
			t := part.Text
			if strings.TrimSpace(t) == "" {
				continue
			}
			out = append(out, ContentPart{Type: "text", Text: t})
			hasTextPart = true
		case "image_url":
			if part.ImageURL == nil {
				return "", nil, nil, fmt.Errorf("image_url part missing image_url")
			}
			normalized, err := normalizeImageURLPart(*part.ImageURL)
			if err != nil {
				return "", nil, nil, err
			}
			imageCount++
			if imageCount > MaxUserImages {
				return "", nil, nil, fmt.Errorf("too many images (max %d)", MaxUserImages)
			}
			out = append(out, ContentPart{Type: "image_url", ImageURL: &normalized})
		default:
			return "", nil, nil, fmt.Errorf("unsupported content part type %q", part.Type)
		}
	}

	if text != "" && !hasTextPart {
		out = append([]ContentPart{{Type: "text", Text: text}}, out...)
		hasTextPart = true
	}
	if len(out) == 0 && len(normalizedRefs) == 0 {
		return "", nil, nil, fmt.Errorf("content is required")
	}
	if len(out) > MaxUserContentParts {
		return "", nil, nil, fmt.Errorf("too many content parts (max %d)", MaxUserContentParts)
	}

	summary := MessageTextFromParts(out)
	if !hasTextPart && imageCount > 0 {
		return summary, out, normalizedRefs, nil
	}
	return summary, out, normalizedRefs, nil
}

// BuildUserMessage 构造 role=user 消息；name 仅作为兼容字段，结构化来源
// 由 MessageSourceForUserName 自动生成，空串仍不写入 wire name。
func BuildUserMessage(text string, parts []ContentPart, name string) (Message, error) {
	summary, normalized, err := NormalizeUserInput(text, parts)
	if err != nil {
		return Message{}, err
	}
	source, provenance := MessageSourceForUserName(name)
	m := UserMessageWithSource(summary, name, source, &provenance)
	if len(normalized) > 0 {
		m.ContentParts = normalized
	}
	return m, nil
}

// BuildUserMessageWithFileReferences constructs a durable user message while
// keeping file references out of the visible text content.
func BuildUserMessageWithFileReferences(text string, parts []ContentPart, name string, refs []FileReference) (Message, error) {
	summary, normalized, normalizedRefs, err := NormalizeUserInputWithFileReferences(text, parts, refs)
	if err != nil {
		return Message{}, err
	}
	source, provenance := MessageSourceForUserName(name)
	m := UserMessageWithSource(summary, name, source, &provenance)
	if len(normalized) > 0 {
		m.ContentParts = normalized
	}
	if len(normalizedRefs) > 0 {
		m.FileReferences = normalizedRefs
	}
	return m, nil
}

// MessageTextSummary 返回消息的纯文本摘要（预览 / 列表 / 压缩用）。
func MessageTextSummary(m Message) string {
	if s := strings.TrimSpace(m.Content); s != "" {
		return s
	}
	return MessageTextFromParts(m.ContentParts)
}

// MessageTextFromParts 拼接 text 类型 part。
func MessageTextFromParts(parts []ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			continue
		}
		if t := strings.TrimSpace(part.Text); t != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(t)
		}
	}
	return b.String()
}

// MessageHasImages 判断消息是否含 image_url part。
func MessageHasImages(m Message) bool {
	for _, part := range m.ContentParts {
		if part.Type == "image_url" && part.ImageURL != nil && strings.TrimSpace(part.ImageURL.URL) != "" {
			return true
		}
	}
	return false
}

const textOnlyImageOmissionNotice = "[图片内容未发送：当前模型未启用图片输入支持。]"

// PrepareMessagesForTextOnly creates the model-facing copy used when the
// active Agent/profile does not support image input. Persisted history keeps
// its original image parts for the UI and for a later vision-capable profile;
// only the outbound request copy is reduced to text so stale image history
// cannot make an otherwise valid text request fail at the provider.
func PrepareMessagesForTextOnly(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, len(messages))
	for i, message := range messages {
		if !MessageHasImages(message) {
			out[i] = message
			continue
		}
		out[i] = CloneMessage(message)
		text := strings.TrimSpace(out[i].Content)
		if partsText := strings.TrimSpace(MessageTextFromParts(out[i].ContentParts)); partsText != "" {
			text = partsText
		}
		if text == "" {
			text = textOnlyImageOmissionNotice
		}
		out[i].Content = text
		out[i].ContentParts = nil
	}
	return out
}

// EstimateMessageContentTokens 粗算单条 message 的 content token（含图片固定开销）。
func EstimateMessageContentTokens(m Message) int {
	m = messageWithFileReferencePrompt(m)
	total := tokensEstimateText(m.Content)
	if len(m.ContentParts) == 0 {
		return total
	}
	seenText := strings.TrimSpace(m.Content) != ""
	for _, part := range m.ContentParts {
		switch part.Type {
		case "text":
			if seenText {
				continue
			}
			total += tokensEstimateText(part.Text)
		case "image_url":
			total += 512
		}
	}
	return total
}

func tokensEstimateText(text string) int {
	return EstimateTextTokens(text)
}

func normalizeImageURLPart(part ImageURLPart) (ImageURLPart, error) {
	rawURL := strings.TrimSpace(part.URL)
	if rawURL == "" {
		return ImageURLPart{}, fmt.Errorf("image url is required")
	}
	detail := strings.TrimSpace(part.Detail)
	if detail != "" {
		switch strings.ToLower(detail) {
		case "auto", "low", "high":
		default:
			return ImageURLPart{}, fmt.Errorf("invalid image detail %q", detail)
		}
	}
	if strings.HasPrefix(strings.ToLower(rawURL), "data:") {
		if err := validateDataImageURL(rawURL); err != nil {
			return ImageURLPart{}, err
		}
		return ImageURLPart{URL: rawURL, Detail: detail}, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ImageURLPart{}, fmt.Errorf("invalid image url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ImageURLPart{}, fmt.Errorf("image url must be http(s) or data URI")
	}
	return ImageURLPart{URL: rawURL, Detail: detail}, nil
}

func validateDataImageURL(raw string) error {
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return fmt.Errorf("invalid data image url")
	}
	meta := strings.ToLower(raw[:comma])
	if !strings.HasPrefix(meta, "data:image/") {
		return fmt.Errorf("unsupported image mime in data url")
	}
	switch {
	case strings.Contains(meta, "image/jpeg"):
	case strings.Contains(meta, "image/png"):
	case strings.Contains(meta, "image/gif"):
	case strings.Contains(meta, "image/webp"):
	default:
		return fmt.Errorf("unsupported image mime in data url")
	}
	payload := raw[comma+1:]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("invalid base64 image data")
	}
	if len(decoded) > MaxImageBytes {
		return fmt.Errorf("image too large (max %d bytes)", MaxImageBytes)
	}
	return nil
}

func contentPartToMap(part ContentPart) map[string]any {
	switch part.Type {
	case "text":
		return map[string]any{"type": "text", "text": part.Text}
	case "image_url":
		if part.ImageURL == nil {
			return map[string]any{"type": "image_url", "image_url": map[string]any{}}
		}
		image := map[string]any{"url": part.ImageURL.URL}
		if d := strings.TrimSpace(part.ImageURL.Detail); d != "" {
			image["detail"] = d
		}
		return map[string]any{"type": "image_url", "image_url": image}
	default:
		return map[string]any{"type": part.Type}
	}
}
