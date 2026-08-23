// Package skills 扫描 .runtime/skills 并提供 metadata/body 读取（对齐 Python harness/skills）。
package skills

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// CatalogBloatTokenThreshold 为 skills 元数据或任一 SKILL 正文估算 token 超过该值时 UI 提示精简。
const CatalogBloatTokenThreshold = 4000

// LoadSkillsMetadataPrefix 为可用 skills 目录的 token 估算前缀。
const LoadSkillsMetadataPrefix = "\n\n可用 skills（name: description）：\n"

// CatalogTokenStats 为 skills 目录 token 估算分项（避免把未加载正文与 system prompt 重复计数）。
type CatalogTokenStats struct {
	// MetadataTokens：system prompt 尾部 skills 目录的元数据（name + description 列表）。
	MetadataTokens int
	// MaxBodyTokens：单个 SKILL.md 正文的最大估算 token（load 后进入独立 skill context，受 max_in_prompt 限制）。
	MaxBodyTokens int
}

// CatalogTiming is a low-cardinality diagnostic snapshot for Skills catalog
// costs. Durations are nanoseconds and are observational only: they never
// enter the model prompt, tool schema, revision, or cache key.
type CatalogTiming struct {
	MetadataScanCount        uint64 `json:"metadata_scan_count"`
	MetadataScanDurationNS   int64  `json:"metadata_scan_duration_ns"`
	BodyReadCount            uint64 `json:"body_read_count"`
	BodyReadDurationNS       int64  `json:"body_read_duration_ns"`
	BodyReadBytes            uint64 `json:"body_read_bytes"`
	BodyCacheHitCount        uint64 `json:"body_cache_hit_count"`
	BoundaryDigestCount      uint64 `json:"boundary_digest_count"`
	BoundaryDigestDurationNS int64  `json:"boundary_digest_duration_ns"`
	TokenEstimateCount       uint64 `json:"token_estimate_count"`
	TokenEstimateDurationNS  int64  `json:"token_estimate_duration_ns"`
}

// LoadedSkill 为会话已加载 skill 元信息（持久化在 session）。
type LoadedSkill struct {
	SkillName     string `json:"skill_name"`
	Description   string `json:"description"`
	DirectoryName string `json:"directory_name,omitempty"`
}

// SkillLoadRejection explains why a requested skill was not activated.
type SkillLoadRejection struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// SkillLoadResult is the model-facing diagnostic projection of a load
// request. The session still stores only the successfully loaded set.
type SkillLoadResult struct {
	Requested []string             `json:"requested"`
	Loaded    []LoadedSkill        `json:"loaded_skills"`
	Rejected  []SkillLoadRejection `json:"rejected,omitempty"`
}

// AvailableSkillsPage is the bounded, metadata-only projection used by the
// optional list_available_skills experiment. It intentionally reuses
// LoadedSkill so directory and logical names have the same shape everywhere.
type AvailableSkillsPage struct {
	CatalogRevision string
	Query           string
	Skills          []LoadedSkill
	HasMore         bool
	NextCursor      string
}

// Definition 为磁盘 skill 定义。
type Definition struct {
	SkillName     string
	DirectoryName string
	Description   string
	Content       string
}

// LoadedSkillContent is the model-facing body of an already selected skill.
// It is deliberately separate from LoadedSkill: the latter is durable
// session metadata while this type is read only when the body must be
// injected into a model-visible context item.
type LoadedSkillContent struct {
	LoadedSkill
	Content string
}

// Catalog 提供 skill 目录扫描与 prompt 渲染。
type Catalog struct {
	root        string
	maxInPrompt int
	enabled     bool
	// frozen marks a Turn-scoped view. A frozen view keeps the metadata and
	// the expected file digest captured at the human-Turn boundary; it never
	// silently switches to a newer on-disk definition mid-Turn.
	frozen           bool
	frozenRevision   string
	frozenDefs       []Definition
	frozenBodyDigest map[string]string

	// restrictVisible 为 true 时仅暴露 visible 中的 skill；false 表示不限制（全部可见）。
	restrictVisible bool
	visible         map[string]struct{}

	mu     sync.RWMutex
	cache  catalogListCache
	bodies map[string]catalogBodyCache

	timingMu sync.Mutex
	timing   CatalogTiming
}

type catalogListCache struct {
	sig   string
	defs  []Definition
	valid bool
}

type catalogBodyCache struct {
	sig     string
	content string
	loaded  bool
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

// TimingSnapshot returns cumulative in-process catalog diagnostics. It is
// intended for tests and runtime observability, not for model-visible data.
func (c *Catalog) TimingSnapshot() CatalogTiming {
	if c == nil {
		return CatalogTiming{}
	}
	c.timingMu.Lock()
	defer c.timingMu.Unlock()
	return c.timing
}

func (c *Catalog) recordMetadataScan(duration time.Duration) {
	if c == nil {
		return
	}
	c.timingMu.Lock()
	c.timing.MetadataScanCount++
	c.timing.MetadataScanDurationNS += duration.Nanoseconds()
	c.timingMu.Unlock()
}

func (c *Catalog) recordBodyRead(duration time.Duration, bytes int) {
	if c == nil {
		return
	}
	c.timingMu.Lock()
	c.timing.BodyReadCount++
	c.timing.BodyReadDurationNS += duration.Nanoseconds()
	if bytes > 0 {
		c.timing.BodyReadBytes += uint64(bytes)
	}
	c.timingMu.Unlock()
}

func (c *Catalog) recordBodyCacheHit() {
	if c == nil {
		return
	}
	c.timingMu.Lock()
	c.timing.BodyCacheHitCount++
	c.timingMu.Unlock()
}

func (c *Catalog) recordBoundaryDigest(duration time.Duration) {
	if c == nil {
		return
	}
	c.timingMu.Lock()
	c.timing.BoundaryDigestCount++
	c.timing.BoundaryDigestDurationNS += duration.Nanoseconds()
	c.timingMu.Unlock()
}

func (c *Catalog) recordTokenEstimate(duration time.Duration) {
	if c == nil {
		return
	}
	c.timingMu.Lock()
	c.timing.TokenEstimateCount++
	c.timing.TokenEstimateDurationNS += duration.Nanoseconds()
	c.timingMu.Unlock()
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

// Revision returns the stable on-disk catalog signature used by the catalog
// cache. It changes when a skill file is added, removed, resized, or its
// modification time changes, allowing callers to publish a next-Turn change
// event without rebuilding the runtime mid-Turn.
func (c *Catalog) Revision() string {
	if c == nil || !c.enabled || strings.TrimSpace(c.root) == "" {
		return ""
	}
	if c.frozen {
		return c.frozenRevision
	}
	sig, err := c.listSignature()
	if err != nil {
		return ""
	}
	return sig
}

// NewTurnView creates an immutable Catalog view for one human Turn.
//
// The metadata scan remains lazy with respect to Skill bodies: the view reads
// body bytes only to capture a boundary digest. The body itself is still
// loaded only when a Skill is selected or rendered. Boundary hashing is done
// once per view, not once per model Step, so an external edit cannot be
// silently introduced by a later load_skills call in the same Turn.
func (c *Catalog) NewTurnView() *Catalog {
	if c == nil {
		return nil
	}
	defs := c.listDefinitions()
	statRevision := c.Revision()
	bodyDigests := make(map[string]string, len(defs))
	digestStarted := time.Now()
	for _, def := range defs {
		digest, ok := digestFile(filepath.Join(c.root, def.DirectoryName, "SKILL.md"))
		if !ok {
			// Keep an explicit marker so a later load fails with
			// catalog_changed/read_failed rather than falling back to live data.
			digest = "!unavailable"
		}
		bodyDigests[def.DirectoryName] = digest
	}
	c.recordBoundaryDigest(time.Since(digestStarted))
	frozenRevision := ""
	if c.enabled && strings.TrimSpace(c.root) != "" {
		frozenRevision = turnViewRevision(statRevision, defs, bodyDigests)
	}
	timing := c.TimingSnapshot()
	return &Catalog{
		root:             c.root,
		maxInPrompt:      c.maxInPrompt,
		enabled:          c.enabled,
		frozen:           true,
		frozenRevision:   frozenRevision,
		frozenDefs:       cloneDefinitions(defs),
		frozenBodyDigest: cloneStringMap(bodyDigests),
		restrictVisible:  c.restrictVisible,
		visible:          cloneStringSet(c.visible),
		timing:           timing,
	}
}

// List 扫描 `{root}/*/SKILL.md` 并返回全部 skill 元数据。正文按需加载。
//
// 结果按各 SKILL.md 的目录名+mtime+size 签名缓存；磁盘未变时复用内存列表，避免每步 tool loop 重复读盘。
func (c *Catalog) List() []Definition {
	return c.applyVisible(c.listDefinitions())
}

func (c *Catalog) listDefinitions() []Definition {
	if !c.enabled || c.root == "" {
		return nil
	}
	if c.frozen {
		return cloneDefinitions(c.frozenDefs)
	}
	sig, err := c.listSignature()
	if err != nil {
		return nil
	}
	c.mu.RLock()
	if c.cache.valid && c.cache.sig == sig {
		out := cloneDefinitions(c.cache.defs)
		c.mu.RUnlock()
		return out
	}
	c.mu.RUnlock()

	out := c.scanDefinitions()
	c.mu.Lock()
	c.cache = catalogListCache{sig: sig, defs: cloneDefinitions(out), valid: true}
	c.bodies = nil
	c.mu.Unlock()
	return out
}

// ListMetadata 返回 skill_name/description 列表。
func (c *Catalog) ListMetadata() []LoadedSkill {
	defs := c.List()
	out := make([]LoadedSkill, 0, len(defs))
	for _, d := range defs {
		out = append(out, LoadedSkill{SkillName: d.SkillName, Description: d.Description, DirectoryName: d.DirectoryName})
	}
	return out
}

// ListAvailableSkills returns a stable, bounded metadata page. It never reads
// Skill bodies and applies the Catalog visibility policy before matching. The
// cursor is an opaque base64-encoded offset valid for the current Catalog
// view; callers should restart from the first page after a revision changes.
func (c *Catalog) ListAvailableSkills(query string, limit int, cursor string) (AvailableSkillsPage, error) {
	page := AvailableSkillsPage{
		CatalogRevision: c.Revision(),
		Query:           strings.TrimSpace(query),
		Skills:          make([]LoadedSkill, 0),
	}
	if c == nil || !c.Enabled() {
		return page, fmt.Errorf("skills_disabled")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	offset, err := decodeSkillsCursor(cursor)
	if err != nil {
		return page, err
	}
	defs := c.List()
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].DirectoryName == defs[j].DirectoryName {
			return defs[i].SkillName < defs[j].SkillName
		}
		return defs[i].DirectoryName < defs[j].DirectoryName
	})
	needle := strings.ToLower(page.Query)
	matched := make([]LoadedSkill, 0, len(defs))
	for _, def := range defs {
		if needle != "" &&
			!strings.Contains(strings.ToLower(def.SkillName), needle) &&
			!strings.Contains(strings.ToLower(def.DirectoryName), needle) &&
			!strings.Contains(strings.ToLower(def.Description), needle) {
			continue
		}
		matched = append(matched, LoadedSkill{
			SkillName:     def.SkillName,
			Description:   def.Description,
			DirectoryName: def.DirectoryName,
		})
	}
	if offset > len(matched) {
		return page, fmt.Errorf("invalid_cursor")
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	page.Skills = append(page.Skills, matched[offset:end]...)
	page.HasMore = end < len(matched)
	if page.HasMore {
		page.NextCursor = encodeSkillsCursor(end)
	}
	return page, nil
}

func encodeSkillsCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeSkillsCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(cursor))
	if err != nil {
		return 0, fmt.Errorf("invalid_cursor")
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid_cursor")
	}
	return offset, nil
}

// SelectByName 按目录名查找 skill。
func (c *Catalog) SelectByName(skillName string) (Definition, bool) {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return Definition{}, false
	}
	matches := c.visibleMatches(c.List(), name)
	if len(matches) != 1 {
		return Definition{}, false
	}
	d := matches[0]
	body, ok := c.loadBody(d)
	if !ok {
		return Definition{}, false
	}
	d.Content = body
	return d, true
}

func (c *Catalog) applyVisible(defs []Definition) []Definition {
	if c == nil || !c.restrictVisible {
		return defs
	}
	out := make([]Definition, 0, len(defs))
	for _, d := range defs {
		_, bySkillName := c.visible[d.SkillName]
		_, byDirectory := c.visible[d.DirectoryName]
		if bySkillName || byDirectory {
			out = append(out, d)
		}
	}
	return out
}

// EstimateCatalogStats 估算 skills 目录 token 分项。
//
// 注意：目录元数据进入启用 skills 的 Agent system prompt 尾部；skill 正文仅在
// load 后写入独立 skill context。因此 skills_catalog_estimated_tokens 只反映
// MetadataTokens，正文膨胀告警另看 MaxBodyTokens。
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

// BloatDisplayTokens 返回用于 UI 告警展示的 token 数（元数据与最大正文中的较大值）。
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
	started := time.Now()
	defer func() { c.recordTokenEstimate(time.Since(started)) }()
	defs := c.List()
	for i := range defs {
		if body, ok := c.loadBody(defs[i]); ok {
			defs[i].Content = body
		}
	}
	return EstimateCatalogStats(defs)
}

// EstimateCatalogMetadataTokens 返回 system prompt 尾部 catalog 元数据的估算 token（API skills_catalog_estimated_tokens）。
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
	nameCounts := make(map[string]int, len(defs))
	for _, d := range defs {
		nameCounts[d.SkillName]++
	}
	var b strings.Builder
	for _, d := range defs {
		name := d.SkillName
		if nameCounts[d.SkillName] > 1 && d.DirectoryName != d.SkillName {
			name = fmt.Sprintf("%s（目录：%s）", d.SkillName, d.DirectoryName)
		}
		desc := strings.TrimSpace(d.Description)
		if desc == "" {
			b.WriteString(fmt.Sprintf("- %s\n", name))
		} else {
			b.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
		}
	}
	return strings.TrimSpace(b.String())
}

// visibleMatches returns the definitions addressable by name after the
// optional visibility allowlist has been applied. Callers intentionally treat
// more than one match as ambiguous instead of silently selecting one based on
// filesystem ordering.
func (c *Catalog) visibleMatches(defs []Definition, name string) []Definition {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	out := make([]Definition, 0, 1)
	for _, d := range defs {
		if d.SkillName != name && d.DirectoryName != name {
			continue
		}
		if c.restrictVisible {
			_, bySkillName := c.visible[d.SkillName]
			_, byDirectory := c.visible[d.DirectoryName]
			if !bySkillName && !byDirectory {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

// RenderLoadedSection renders a legacy inline body section for diagnostics and
// compatibility. Runtime model requests must use ReadLoadedSkillContents and
// the turn package's independent skill context messages instead.
func (c *Catalog) RenderLoadedSection(loaded []LoadedSkill) string {
	if len(loaded) == 0 {
		return ""
	}
	var b strings.Builder
	for _, item := range loaded {
		lookup := item.SkillName
		if strings.TrimSpace(item.DirectoryName) != "" {
			lookup = item.DirectoryName
		}
		def, ok := c.SelectByName(lookup)
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

// ReadLoadedSkillContents reads the bodies for the selected skills in the
// supplied order. It preserves the frozen Catalog view boundary and returns
// only definitions whose body can be read successfully. The caller decides
// how the bodies are represented in model history; this method intentionally
// does not render a system-prompt section.
func (c *Catalog) ReadLoadedSkillContents(loaded []LoadedSkill) []LoadedSkillContent {
	if c == nil || len(loaded) == 0 {
		return nil
	}
	out := make([]LoadedSkillContent, 0, len(loaded))
	for _, item := range loaded {
		lookup := item.SkillName
		if strings.TrimSpace(item.DirectoryName) != "" {
			lookup = item.DirectoryName
		}
		def, ok := c.SelectByName(lookup)
		if !ok || strings.TrimSpace(def.Content) == "" {
			continue
		}
		out = append(out, LoadedSkillContent{
			LoadedSkill: item,
			Content:     strings.TrimSpace(def.Content),
		})
	}
	return out
}

// SetLoadedSkills 按名称整组替换 loaded skills（load_skills 语义）。
func (c *Catalog) SetLoadedSkills(names []string) []LoadedSkill {
	return c.SetLoadedSkillsDetailed(names).Loaded
}

// SetLoadedSkillsDetailed applies the load_skills replace semantics while
// preserving enough diagnostics for the model and UI to distinguish invalid,
// invisible and capacity-limited requests.
func (c *Catalog) SetLoadedSkillsDetailed(names []string) SkillLoadResult {
	result := SkillLoadResult{Requested: make([]string, 0, len(names))}
	if !c.enabled {
		for _, raw := range names {
			name := strings.TrimSpace(raw)
			if name != "" {
				result.Requested = append(result.Requested, name)
				result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: "skills_disabled"})
			}
		}
		return result
	}
	seen := make(map[string]struct{})
	// A skill may be addressed by both its frontmatter name and its
	// directory name. Track the canonical directory after resolution so those
	// aliases cannot consume two prompt slots or register the same hooks twice.
	loadedDirectories := make(map[string]struct{})
	defs := c.listDefinitions()
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		result.Requested = append(result.Requested, name)
		if _, ok := seen[name]; ok {
			result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: "duplicate"})
			continue
		}
		seen[name] = struct{}{}

		matches := c.visibleMatches(defs, name)
		if len(matches) == 0 {
			foundAnywhere := false
			for _, candidate := range defs {
				if candidate.SkillName == name || candidate.DirectoryName == name {
					foundAnywhere = true
					break
				}
			}
			if foundAnywhere && c.restrictVisible {
				result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: "not_visible"})
				continue
			}
			result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: "not_found"})
			continue
		}
		if len(matches) > 1 {
			result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: "ambiguous"})
			continue
		}
		def := matches[0]
		if len(result.Loaded) >= c.maxInPrompt {
			result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: "max_in_prompt"})
			continue
		}
		if _, ok := loadedDirectories[def.DirectoryName]; ok {
			result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: "duplicate"})
			continue
		}
		if _, ok, reason := c.loadBodyDetailed(def); !ok {
			if reason == "" {
				reason = "read_failed"
			}
			result.Rejected = append(result.Rejected, SkillLoadRejection{Name: name, Reason: reason})
			continue
		}
		loadedDirectories[def.DirectoryName] = struct{}{}
		result.Loaded = append(result.Loaded, LoadedSkill{
			SkillName:     def.SkillName,
			Description:   def.Description,
			DirectoryName: def.DirectoryName,
		})
	}
	return result

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
		_, dropByName := remove[item.SkillName]
		_, dropByDirectory := remove[item.DirectoryName]
		if dropByName || dropByDirectory {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (c *Catalog) scanDefinitions() []Definition {
	started := time.Now()
	defer func() { c.recordMetadataScan(time.Since(started)) }()
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
		def, ok := c.readSkillMetadata(filepath.Join(c.root, dirName, "SKILL.md"), dirName)
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

// loadBody reads a skill body only when the caller actually needs the skill
// content. Catalog listing and prompt metadata therefore do not load every
// SKILL.md body. Bodies are cached against the same catalog signature as the
// metadata cache and are invalidated together with catalog changes.
func (c *Catalog) loadBody(def Definition) (string, bool) {
	body, ok, _ := c.loadBodyDetailed(def)
	return body, ok
}

func (c *Catalog) loadBodyDetailed(def Definition) (string, bool, string) {
	if c == nil || strings.TrimSpace(def.DirectoryName) == "" {
		return "", false, "read_failed"
	}
	sig := c.Revision()
	if !c.frozen {
		var err error
		sig, err = c.listSignature()
		if err != nil {
			return "", false, "read_failed"
		}
	}
	c.mu.RLock()
	if entry, ok := c.bodies[def.DirectoryName]; ok && entry.loaded && entry.sig == sig {
		c.mu.RUnlock()
		c.recordBodyCacheHit()
		return entry.content, true, ""
	}
	c.mu.RUnlock()

	path := filepath.Join(c.root, def.DirectoryName, "SKILL.md")
	readStarted := time.Now()
	raw, err := os.ReadFile(path)
	c.recordBodyRead(time.Since(readStarted), len(raw))
	if err != nil {
		if c.frozen {
			return "", false, "catalog_changed"
		}
		return "", false, "read_failed"
	}
	if c.frozen {
		expected, ok := c.frozenBodyDigest[def.DirectoryName]
		if !ok || expected == "!unavailable" || digestBytes(raw) != expected {
			return "", false, "catalog_changed"
		}
	}
	_, body := parseFrontmatter(string(raw))
	c.mu.Lock()
	if c.bodies == nil {
		c.bodies = make(map[string]catalogBodyCache)
	}
	if c.frozen || (c.cache.valid && c.cache.sig == sig) {
		c.bodies[def.DirectoryName] = catalogBodyCache{sig: sig, content: body, loaded: true}
	}
	c.mu.Unlock()
	return body, true, ""
}

func digestFile(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return digestBytes(raw), true
}

func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func turnViewRevision(statRevision string, defs []Definition, bodyDigests map[string]string) string {
	var b strings.Builder
	b.WriteString(statRevision)
	for _, def := range defs {
		b.WriteByte('\n')
		b.WriteString(def.DirectoryName)
		b.WriteByte('|')
		b.WriteString(def.SkillName)
		b.WriteByte('|')
		b.WriteString(bodyDigests[def.DirectoryName])
	}
	return digestBytes([]byte(b.String()))
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringSet(in map[string]struct{}) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}

// readSkillMetadata reads only frontmatter needed for the available-skills
// catalog. A full body read is deferred to loadBody.
func (c *Catalog) readSkillMetadata(path, dirName string) (Definition, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Definition{}, false
	}
	defer file.Close()

	def := Definition{SkillName: dirName, DirectoryName: dirName}
	reader := bufio.NewReader(file)
	first, err := reader.ReadString('\n')
	first = strings.TrimSpace(normalizeSkillTextNewlines(first))
	if err != nil && err != io.EOF && first == "" {
		return Definition{}, false
	}
	if first != "---" {
		return def, true
	}

	lines := make([]string, 0, 8)
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimRight(normalizeSkillTextNewlines(line), "\n")
		if strings.TrimSpace(line) == "---" {
			meta := parseSimpleYAML(strings.Join(lines, "\n"))
			if name := metaString(meta, "name"); name != "" {
				def.SkillName = name
			}
			def.Description = metaString(meta, "description")
			return def, true
		}
		lines = append(lines, line)
		if readErr == io.EOF {
			return def, true
		}
		if readErr != nil {
			return Definition{}, false
		}
	}
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
