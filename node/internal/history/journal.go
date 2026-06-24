// Package history 提供原始 OpenAI 消息 JSONL 审计侧车（对齐 Python raw_message_journal）。
package history

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

const maxSessionFilenamePartLen = 200

var sessionIDSanitizePattern = regexp.MustCompile(`[^\w.\-]+`)

// Journal 按 session + 自然日追加 JSONL 原始消息记录（`<baseDir>/YYYYMMDD/<session>.jsonl`）。
type Journal struct {
	enabled bool
	baseDir string
	logger  *slog.Logger
}

// NewJournal 构造 JSONL 侧车；enabled=false 时仍执行消息规范化但不写盘。
func NewJournal(enabled bool, baseDir string, logger *slog.Logger) *Journal {
	return &Journal{
		enabled: enabled,
		baseDir: strings.TrimSpace(baseDir),
		logger:  logx.OrDefault(logger),
	}
}

// Enabled 返回是否写入 JSONL 文件。
func (j *Journal) Enabled() bool {
	return j != nil && j.enabled
}

// RecordAppend 将一条「插入瞬间」的消息快照追加写入 JSONL。

// 逻辑：
// 1. 开关关闭或 session_id 为空则直接返回；
// 2. 构造 recorded_at 与 message 快照；
// 3. 确保父目录存在后追加一行 JSON。

// 关键分支：写入/序列化失败仅打 warning，不向上抛异常。
func (j *Journal) RecordAppend(sessionID string, message llm.Message) {
	if j == nil || !j.enabled {
		return
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	path := journalFilePath(j.baseDir, sid)
	record := map[string]any{
		"recorded_at": formatRecordedAt(time.Now()),
		"message":     messageToJournalPayload(message),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		j.safeLogger().Warn("raw message journal serialize failed", "error", err, "path", path)
		return
	}
	if err := appendJournalLine(path, string(raw)+"\n"); err != nil {
		j.safeLogger().Warn("raw message journal write failed", "error", err, "path", path)
	}
}

// AppendMessage 在 history 末尾追加一条已规范化的消息并同步写入 JSONL。
//
// 调用方（Orchestrator）须先经 llm.Client.NormalizeAssistant 处理 assistant 消息。
func (j *Journal) AppendMessage(sessionID string, history *[]llm.Message, message llm.Message) {
	if history == nil {
		return
	}
	snapshot := llm.CloneMessage(message)
	*history = append(*history, message)
	if j != nil {
		j.RecordAppend(sessionID, snapshot)
	}
}

// InsertMessage 在 history 指定下标插入一条已规范化的消息并同步写入 JSONL。
func (j *Journal) InsertMessage(sessionID string, history *[]llm.Message, index int, message llm.Message) {
	if history == nil {
		return
	}
	snapshot := llm.CloneMessage(message)
	if index < 0 {
		index = 0
	}
	if index > len(*history) {
		index = len(*history)
	}
	out := append([]llm.Message(nil), (*history)[:index]...)
	out = append(out, message)
	out = append(out, (*history)[index:]...)
	*history = out
	if j != nil {
		j.RecordAppend(sessionID, snapshot)
	}
}

func (j *Journal) safeLogger() *slog.Logger {
	if j == nil || j.logger == nil {
		return slog.Default()
	}
	return j.logger
}

func sanitizeSessionIDForFilename(sessionID string) string {
	raw := strings.TrimSpace(sessionID)
	if raw == "" {
		return "unknown_session"
	}
	safe := sessionIDSanitizePattern.ReplaceAllString(raw, "_")
	collapsed := strings.Trim(safe, "._-")
	if collapsed == "" {
		return "session"
	}
	if len(safe) > maxSessionFilenamePartLen {
		safe = safe[:maxSessionFilenamePartLen]
	}
	return safe
}

func journalFilePath(baseDir, sessionID string) string {
	at := time.Now()
	day := at.Format("20060102")
	safeSID := sanitizeSessionIDForFilename(sessionID)
	return filepath.Join(baseDir, day, safeSID+".jsonl")
}

// JournalRelativePath 返回相对工作区根的 JSONL 审计路径（history/YYYYMMDD/<session>.jsonl）。
func JournalRelativePath(sessionID string, at time.Time) string {
	day := at.Format("20060102")
	safeSID := sanitizeSessionIDForFilename(sessionID)
	return fmt.Sprintf("history/%s/%s.jsonl", day, safeSID)
}

func formatRecordedAt(t time.Time) string {
	local := t.In(time.Local)
	ms := local.Nanosecond() / int(time.Millisecond)
	_, offset := local.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	mins := (offset % 3600) / 60
	return fmt.Sprintf(
		"%s.%03d%s%02d:%02d",
		local.Format("2006-01-02T15:04:05"),
		ms,
		sign,
		hours,
		mins,
	)
}

func appendJournalLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}
