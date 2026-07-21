package promptcontext

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	soulFile   = "soul.md"
	userFile   = "user.md"
	customFile = "custom.md"
)

type cachedFile struct {
	content string
	mtime   int64
}

// Filter 控制侧车 / 长期记忆是否注入；nil 指针表示默认启用。
type Filter struct {
	SoulEnabled     *bool
	UserEnabled     *bool
	CustomEnabled   *bool
	LongTermEnabled *bool
}

func flagOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// Reader 读取 `.runtime/prompt_context/` 侧车 Markdown 与长期记忆，带 mtime 缓存。
type Reader struct {
	runtimeDir string
	filter     Filter
	mu         sync.Mutex
	cache      map[string]cachedFile
}

// NewReader 绑定 `.runtime` 根目录（非 prompt_context 子目录本身）。
func NewReader(runtimeDir string) *Reader {
	return &Reader{
		runtimeDir: strings.TrimSpace(runtimeDir),
		cache:      make(map[string]cachedFile),
	}
}

// SetFilter 设置侧车注入开关（缺省全开）。
func (r *Reader) SetFilter(f Filter) {
	if r == nil {
		return
	}
	r.filter = f
}

// Dir 返回 prompt_context 绝对路径并确保目录存在。
func (r *Reader) Dir() (string, error) {
	if r.runtimeDir == "" {
		return "", nil
	}
	dir := filepath.Join(r.runtimeDir, "prompt_context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// EnsureSidecarFiles 确保 soul/user/custom 存在；缺失则创建空 UTF-8 文件，不覆盖已有内容。
func (r *Reader) EnsureSidecarFiles() {
	dir, err := r.Dir()
	if err != nil || dir == "" {
		return
	}
	for _, name := range []string{soulFile, userFile, customFile} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			continue
		}
		if err == nil && info.IsDir() {
			continue
		}
		_ = os.WriteFile(path, []byte{}, 0o644)
	}
}

// ReadSoul 读取 soul.md；空白或缺失返回空串。
func (r *Reader) ReadSoul() string {
	if !flagOrDefault(r.filter.SoulEnabled, true) {
		return ""
	}
	return r.readSidecar(soulFile)
}

// ReadUser 读取 user.md。
func (r *Reader) ReadUser() string {
	if !flagOrDefault(r.filter.UserEnabled, true) {
		return ""
	}
	return r.readSidecar(userFile)
}

// ReadCustom 读取 custom.md。
func (r *Reader) ReadCustom() string {
	if !flagOrDefault(r.filter.CustomEnabled, true) {
		return ""
	}
	return r.readSidecar(customFile)
}

// ReadLongTermMemory 读取 `.runtime/memory/long_term.md`（不存在或空白时不注入）。
func (r *Reader) ReadLongTermMemory() string {
	if !flagOrDefault(r.filter.LongTermEnabled, true) {
		return ""
	}
	if r.runtimeDir == "" {
		return ""
	}
	path := filepath.Join(r.runtimeDir, "memory", "long_term.md")
	return r.readFileCached(path, false)
}

func (r *Reader) readSidecar(filename string) string {
	r.EnsureSidecarFiles()
	dir, err := r.Dir()
	if err != nil || dir == "" {
		return ""
	}
	return r.readFileCached(filepath.Join(dir, filename), true)
}

func (r *Reader) readFileCached(path string, ensureParent bool) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	if ensureParent {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	key, err := filepath.Abs(path)
	if err != nil {
		key = path
	}
	mtime := info.ModTime().UnixNano()
	r.mu.Lock()
	if cached, ok := r.cache[key]; ok && cached.mtime == mtime {
		text := cached.content
		r.mu.Unlock()
		return text
	}
	r.mu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	r.mu.Lock()
	r.cache[key] = cachedFile{content: text, mtime: mtime}
	r.mu.Unlock()
	return text
}

// BuildStableContextSections 拼接 soul / user / long_term 段落（较稳定上下文）。
func (r *Reader) BuildStableContextSections() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	if soul := r.ReadSoul(); soul != "" {
		b.WriteString("\n\n## 以下是你的设定：\n\n")
		b.WriteString(soul)
		b.WriteByte('\n')
	}
	if user := r.ReadUser(); user != "" {
		b.WriteString("\n\n## 以下是用户信息与偏好：\n\n")
		b.WriteString(user)
		b.WriteByte('\n')
	}
	if mem := r.ReadLongTermMemory(); mem != "" {
		b.WriteString("\n\n## 以下是长期记忆：\n\n")
		b.WriteString(mem)
		b.WriteByte('\n')
	}
	return b.String()
}

// BuildCustomSection 拼接 custom.md 临时/专项指令段。
func (r *Reader) BuildCustomSection() string {
	if r == nil {
		return ""
	}
	custom := r.ReadCustom()
	if custom == "" {
		return ""
	}
	return "\n\n## 以下是用户侧追加的临时/专项指令：\n\n" + custom + "\n"
}
