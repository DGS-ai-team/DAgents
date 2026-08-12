// Package skills 扫描 .runtime/skills 并渲染 prompt 段（对齐 Python harness/skills）。
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// CatalogBloatTokenThreshold 为 skills 元数据或任一 SKILL 正文估算 token 超过该值时 TUI 提示精简。
const CatalogBloatTokenThreshold = 4000

// LoadSkillsMetadataPrefix 为 tools enrich 附在 load_skills description 后的固定前缀（须与 tools.loadSkillsMetadataPrefix 一致）。
const LoadSkillsMetadataPrefix = "\n\n可用 skills（name: description）：\n"

// CatalogTokenStats 为 skills 目录 token 估算分项（避免把未加载正文与 system prompt 重复计数）。
type CatalogTokenStats struct {
	// MetadataTokens：每步 tools schema 中 load_skills 附带的 catalog 元数据（name + description 列表）。
	MetadataTokens int
	// MaxBodyTokens：单个 SKILL.md 正文的最大估算 token（load 后进入 system prompt，受 max_in_prompt 限制）。
	MaxBodyTokens int
}

// LoadedSkill 为会话已加载 skill 元信息（持久化在 session）。
type LoadedSkill struct {
	SkillName   string `json:"skill_name"`
	Description string `json:"description"`
}

// Definition 为磁盘 skill 定义。
type Definition struct {
	SkillName   string
	Description string
	Content     string
}

// Catalog 提供 skill 目录扫描与 prompt 渲染。
type Catalog struct {
	root        string
	maxInPrompt int
	enabled     bool

	// restrictVisible 为 true 时仅暴露 visible 中的 skill；false 表示不限制（全部可见）。
	restrictVisible bool
	visible         map[string]struct{}

	mu    sync.RWMutex
	cache catalogListCache
}

type catalogListCache struct {
	sig  string
	defs []Definition
}

// NewCatalog 构造 skill 目录访问器。
func NewCatalog(root string, enabled bool, maxInPrompt int) *Catalog {
	if maxInPrompt <= 0 {
		maxInPrompt = 3
	}
	return &Catalog{
		root:        strings.TrimSpace(root),
		enabled:     enabled,
		maxInPrompt: maxInPrompt,
	}
}

// RestrictVisible 限制 List/Select 仅返回指定名称；names 非 nil 即开启限制（空切片=全部不可见）。
// 传入 nil 表示取消限制（恢复全部可见）。返回接收者以便链式调用。
func (c *Catalog) RestrictVisible(names []string) *Catalog {
	if c == nil {
		return c
	}
	if names == nil {
		c.restrictVisible = false
		c.visible = nil
		return c
	}
	c.restrictVisible = true
	c.visible = make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name != "" {
			c.visible[name] = struct{}{}
		}
	}
	return c
}

// VisibleRestricted 表示是否启用了可见性白名单。
func (c *Catalog) VisibleRestricted() bool {
	return c != nil && c.restrictVisible
}

// Enabled 表示 skills 功能是否开启。
func (c *Catalog) Enabled() bool {
	return c != nil && c.enabled
}

// Root 返回 skills 目录根路径（{fs_root}/skills）。
func (c *Catalog) Root() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.root)
}

// List 扫描 `{root}/*/SKILL.md` 并返回全部 skill 元数据与正文。
//
// 结果按各 SKILL.md 的 name+mtime+size 签名缓存；磁盘未变时复用内存列表，避免每步 tool loop 重复读盘。
func (c *Catalog) List() []Definition {
	if !c.enabled || c.root == "" {
		return nil
	}
	sig, err := c.listSignature()
	if err != nil {
		return nil
	}
	c.mu.RLock()
	if c.cache.sig == sig && c.cache.defs != nil {
		out := cloneDefinitions(c.cache.defs)
		c.mu.RUnlock()
		return c.applyVisible(out)
	}
	c.mu.RUnlock()

	out := c.scanDefinitions()
	c.mu.Lock()
	c.cache = catalogListCache{sig: sig, defs: cloneDefinitions(out)}
	c.mu.Unlock()
	return c.applyVisible(out)
}

// ListMetadata 返回 skill_name/description 列表。
func (c *Catalog) ListMetadata() []LoadedSkill {
	defs := c.List()
	out := make([]LoadedSkill, 0, len(defs))
	for _, d := range defs {
		out = append(out, LoadedSkill{SkillName: d.SkillName, Description: d.Description})
	}
	return out
}

// SelectByName 按目录名查找 skill。
func (c *Catalog) SelectByName(skillName string) (Definition, bool) {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return Definition{}, false
	}
	if c.restrictVisible {
		if _, ok := c.visible[name]; !ok {
			return Definition{}, false
		}
	}
	for _, d := range c.List() {
		if d.SkillName == name {
			return d, true
		}
	}
	return Definition{}, false
}

func (c *Catalog) applyVisible(defs []Definition) []Definition {
	if c == nil || !c.restrictVisible {
		return defs
	}
	out := make([]Definition, 0, len(defs))
	for _, d := range defs {
		if _, ok := c.visible[d.SkillName]; ok {
			out = append(out, d)
		}
	}
	return out
}

// EstimateCatalogStats 估算 skills 目录 token 分项。
//
// 注意：catalog 正文不会进入 tools schema；仅 load 后写入 system prompt（已计入 system_prompt_estimated_tokens）。
// 因此 skills_catalog_estimated_tokens 只反映 MetadataTokens，膨胀告警另看 MaxBodyTokens。
func EstimateCatalogStats(defs []Definition) CatalogTokenStats {
	metaSection := renderMetadataSection(defs)
	stats := CatalogTokenStats{}
	if metaSection != "" {
		stats.MetadataTokens = tokens.EstimateInt(LoadSkillsMetadataPrefix + metaSection)
	}
	for _, d := range defs {
		bodyTokens := tokens.EstimateInt(d.Content)
		if bodyTokens > stats.MaxBodyTokens {
			stats.MaxBodyTokens = bodyTokens
		}
	}
	return stats
}

// BloatDisplayTokens 返回用于 TUI 告警展示的 token 数（元数据与最大正文中的较大值）。
func (s CatalogTokenStats) BloatDisplayTokens() int {
	if s.MaxBodyTokens > s.MetadataTokens {
		return s.MaxBodyTokens
	}
	return s.MetadataTokens
}

// ExceedsBloatThreshold 判断元数据或任一 skill 正文是否超过阈值。
func (s CatalogTokenStats) ExceedsBloatThreshold(threshold int) bool {
	if threshold <= 0 {
		threshold = CatalogBloatTokenThreshold
	}
	return s.MetadataTokens > threshold || s.MaxBodyTokens > threshold
}

// EstimateCatalogStats 返回当前磁盘 catalog 的 token 分项。
func (c *Catalog) EstimateCatalogStats() CatalogTokenStats {
	if c == nil || !c.enabled {
		return CatalogTokenStats{}
	}
	return EstimateCatalogStats(c.List())
}

// EstimateCatalogMetadataTokens 返回 catalog 元数据在 tools schema 中的估算 token（API skills_catalog_estimated_tokens）。
func (c *Catalog) EstimateCatalogMetadataTokens() int {
	return c.EstimateCatalogStats().MetadataTokens
}

// RenderMetadataSection 渲染可用 skills 元数据段。
func (c *Catalog) RenderMetadataSection() string {
	return renderMetadataSection(c.List())
}

func renderMetadataSection(defs []Definition) string {
	if len(defs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, d := range defs {
		desc := strings.TrimSpace(d.Description)
		if desc == "" {
			b.WriteString(fmt.Sprintf("- %s\n", d.SkillName))
		} else {
			b.WriteString(fmt.Sprintf("- %s: %s\n", d.SkillName, desc))
		}
	}
	return strings.TrimSpace(b.String())
}

// RenderLoadedSection 渲染已加载 skill 正文段（不含外层标题，由 prompt 层拼标题）。
func (c *Catalog) RenderLoadedSection(loaded []LoadedSkill) string {
	if len(loaded) == 0 {
		return ""
	}
	var b strings.Builder
	for _, item := range loaded {
		def, ok := c.SelectByName(item.SkillName)
		if !ok {
			continue
		}
		body := strings.TrimSpace(def.Content)
		if body == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n%s\n\n", def.SkillName, body))
	}
	return strings.TrimSpace(b.String())
}

// SetLoadedSkills 按名称整组替换 loaded skills（load_skills 语义）。
func (c *Catalog) SetLoadedSkills(names []string) []LoadedSkill {
	if !c.enabled {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]LoadedSkill, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		def, ok := c.SelectByName(name)
		if !ok {
			continue
		}
		out = append(out, LoadedSkill{SkillName: def.SkillName, Description: def.Description})
		if len(out) >= c.maxInPrompt {
			break
		}
	}
	return out
}

// UnloadSkills 从 loaded 中移除指定名称。
func (c *Catalog) UnloadSkills(loaded []LoadedSkill, names []string) []LoadedSkill {
	remove := make(map[string]struct{})
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name != "" {
			remove[name] = struct{}{}
		}
	}
	out := make([]LoadedSkill, 0, len(loaded))
	for _, item := range loaded {
		if _, drop := remove[item.SkillName]; drop {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (c *Catalog) scanDefinitions() []Definition {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return nil
	}
	out := make([]Definition, 0)
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dirName := strings.TrimSpace(ent.Name())
		if dirName == "" {
			continue
		}
		def, ok := c.readSkill(filepath.Join(c.root, dirName, "SKILL.md"), dirName)
		if ok {
			out = append(out, def)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SkillName < out[j].SkillName })
	return out
}

// listSignature 汇总各子目录 SKILL.md 的目录名、mtime、size，用于判断缓存是否仍有效。
func (c *Catalog) listSignature() (string, error) {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return "", err
	}
	type part struct {
		dir  string
		mod  int64
		size int64
	}
	parts := make([]part, 0)
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dirName := strings.TrimSpace(ent.Name())
		if dirName == "" {
			continue
		}
		st, err := os.Stat(filepath.Join(c.root, dirName, "SKILL.md"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		parts = append(parts, part{dir: dirName, mod: st.ModTime().UnixNano(), size: st.Size()})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].dir < parts[j].dir })
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.dir)
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(p.mod, 10))
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(p.size, 10))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func cloneDefinitions(in []Definition) []Definition {
	if len(in) == 0 {
		return nil
	}
	out := make([]Definition, len(in))
	copy(out, in)
	return out
}

func (c *Catalog) readSkill(path, dirName string) (Definition, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, false
	}
	meta, body := parseFrontmatter(string(raw))
	desc := metaString(meta, "description")
	skillName := dirName
	if name := metaString(meta, "name"); name != "" {
		skillName = name
	}
	return Definition{
		SkillName:   skillName,
		Description: desc,
		Content:     body,
	}, true
}

func parseFrontmatter(text string) (map[string]any, string) {
	text = strings.TrimSpace(normalizeSkillTextNewlines(text))
	if !strings.HasPrefix(text, "---\n") {
		return map[string]any{}, text
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return map[string]any{}, text
	}
	metaBlock := text[4 : 4+end]
	body := strings.TrimSpace(text[4+end+5:])
	return parseSimpleYAML(metaBlock), body
}

func normalizeSkillTextNewlines(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(raw))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}

func parseSimpleYAML(block string) map[string]any {
	out := make(map[string]any)
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, ":") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		lower := strings.ToLower(val)
		switch lower {
		case "true":
			out[key] = true
		case "false":
			out[key] = false
		default:
			out[key] = val
		}
	}
	return out
}
