package workgroup

import (
	"context"
	"errors"
	"sync"
	"strings"
	"time"
)

// CommandExecutor 执行已 accept 的 tool.command；ctx 可被 tool.cancel 取消。
type CommandExecutor func(ctx context.Context, cmd ToolCommand) (resultJSON string, err error)

// CommandHandler 接受 tool.command：先 journal 再返回 ack；不重复执行。
type CommandHandler struct {
	Bindings             BindingStore
	Journal              CommandJournal
	ConnectionGeneration int64
	CatalogRevision      string // 当前 Node manifest revision；空则跳过 catalog 检查
	Tombstones           map[string]ArchiveTombstone
	// Executor 可选；nil 时 D2 仅 accept 不执行（仍计 executions=0→1 在 MarkRunning）
	Executor CommandExecutor

	mu       sync.Mutex
	running  map[string]context.CancelFunc // command_id → cancel（运行中可打断）
}

// AcceptResult 为 accept 结果。
type AcceptResult struct {
	Ack       CommandAck
	Entry     JournalEntry
	Executed  bool
	Rejected  bool
	ErrorCode ErrorCode
}

// Accept 处理首次 command 或重发。
func (h *CommandHandler) Accept(cmd ToolCommand, binding WorkerBinding) (*AcceptResult, error) {
	if cmd.CommandID == "" || cmd.PayloadHash == "" {
		return nil, errf(CodeSchemaMismatch, "command_id/payload_hash required")
	}
	existing, err := h.Journal.Get(cmd.CommandID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.PayloadHash != "" && existing.PayloadHash != cmd.PayloadHash {
			return nil, errf(CodePayloadConflict, "same command_id with different payload_hash")
		}
		// 已被 tool.cancel 标记：禁止执行/恢复执行
		if existing.Status == "canceled" {
			return &AcceptResult{
				Ack: CommandAck{
					CommandID:            cmd.CommandID,
					Status:               existing.Status,
					ConnectionGeneration: h.ConnectionGeneration,
					JournaledAt:          existing.JournaledAt,
				},
				Entry:    *existing,
				Executed: false,
			}, nil
		}
		// D3：accepted 且尚未开始副作用 → 重启后可恢复执行一次
		if existing.Status == "accepted" && existing.Executions == 0 && h.Executor != nil {
			return h.runExecutor(cmd, *existing)
		}
		// 进程重启后 journal 残留 running、但本进程已无执行上下文：升格 indeterminate，
		// 避免只回 tool.ack 导致 Manage 空等 60s。
		if existing.Status == "running" && !h.isTrackedRunning(cmd.CommandID) {
			now := time.Now().UTC().Format(time.RFC3339)
			existing.Status = "indeterminate"
			existing.ErrorCode = string(CodeIndeterminate)
			existing.UpdatedAt = now
			if err := h.Journal.Put(*existing); err != nil {
				return nil, err
			}
			return &AcceptResult{
				Ack: CommandAck{
					CommandID:            cmd.CommandID,
					Status:               existing.Status,
					ConnectionGeneration: h.ConnectionGeneration,
					JournaledAt:          existing.JournaledAt,
				},
				Entry:    *existing,
				Executed: false,
			}, nil
		}
		// 已 journal 且终态/已跑过：返回状态，不重执行
		return &AcceptResult{
			Ack: CommandAck{
				CommandID:            cmd.CommandID,
				Status:               existing.Status,
				ConnectionGeneration: h.ConnectionGeneration,
				JournaledAt:          existing.JournaledAt,
			},
			Entry:    *existing,
			Executed: false,
		}, nil
	}

	if tomb, ok := h.Tombstones[cmd.WorkgroupID]; ok {
		if err := CheckCommandFencing(cmd, binding, h.CatalogRevision, &tomb); err != nil {
			return h.reject(cmd, err)
		}
	} else if err := CheckCommandFencing(cmd, binding, h.CatalogRevision, nil); err != nil {
		return h.reject(cmd, err)
	}

	// 工具名必须在 binding 有效集内
	if len(binding.ToolAllowNames) > 0 {
		allowed := false
		for _, n := range binding.ToolAllowNames {
			if n == cmd.ToolName {
				allowed = true
				break
			}
		}
		if !allowed {
			return h.reject(cmd, errf(CodeNotAuthorized, "tool %q not in binding allow list", cmd.ToolName))
		}
	} else if cmd.ToolName != "" {
		return h.reject(cmd, errf(CodeNotAuthorized, "empty allow list means no tools"))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	entry := JournalEntry{
		CommandID:       cmd.CommandID,
		PayloadHash:     cmd.PayloadHash,
		Status:          "accepted",
		MemberID:        cmd.MemberID,
		WorkgroupID:     cmd.WorkgroupID,
		ToolName:        cmd.ToolName,
		SideEffectClass: cmd.SideEffectClass,
		Executions:      0,
		JournaledAt:     now,
		UpdatedAt:       now,
	}
	if err := h.Journal.Put(entry); err != nil {
		return nil, err
	}

	ack := CommandAck{
		CommandID:            cmd.CommandID,
		Status:               "accepted",
		ConnectionGeneration: h.ConnectionGeneration,
		JournaledAt:          now,
	}

	if h.Executor == nil {
		return &AcceptResult{Ack: ack, Entry: entry, Executed: false}, nil
	}
	return h.runExecutor(cmd, entry)
}

func (h *CommandHandler) runExecutor(cmd ToolCommand, entry JournalEntry) (*AcceptResult, error) {
	// 执行前再查一次：挡 tool.cancel 与 Accept 的竞态
	if cur, err := h.Journal.Get(cmd.CommandID); err == nil && cur != nil && cur.Status == "canceled" {
		return &AcceptResult{
			Ack: CommandAck{
				CommandID:            cmd.CommandID,
				Status:               cur.Status,
				ConnectionGeneration: h.ConnectionGeneration,
				JournaledAt:          cur.JournaledAt,
			},
			Entry:    *cur,
			Executed: false,
		}, nil
	}

	entry.Status = "running"
	entry.Executions = 1
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := h.Journal.Put(entry); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.trackRunning(cmd.CommandID, cancel)
	defer h.untrackRunning(cmd.CommandID)

	resultJSON, execErr := h.Executor(ctx, cmd)
	// 执行中被 cancel：保留 canceled，不覆盖为 succeeded/failed
	if cur, err := h.Journal.Get(cmd.CommandID); err == nil && cur != nil && cur.Status == "canceled" {
		return &AcceptResult{
			Ack: CommandAck{
				CommandID:            cmd.CommandID,
				Status:               cur.Status,
				ConnectionGeneration: h.ConnectionGeneration,
				JournaledAt:          cur.JournaledAt,
			},
			Entry:    *cur,
			Executed: true,
		}, nil
	}
	if execErr != nil {
		if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
			entry.Status = "canceled"
			entry.ErrorCode = string(CodeCanceled)
		} else if we, ok := execErr.(*Error); ok && we.Code == CodeCanceled {
			entry.Status = "canceled"
			entry.ErrorCode = string(CodeCanceled)
		} else if we, ok := execErr.(*Error); ok && we.Code == CodeIndeterminate {
			entry.Status = "indeterminate"
			entry.ErrorCode = string(CodeIndeterminate)
		} else {
			entry.Status = "failed"
			entry.ErrorCode = string(CodeConflict)
			if we, ok := execErr.(*Error); ok {
				entry.ErrorCode = string(we.Code)
			}
		}
	} else {
		entry.Status = "succeeded"
		entry.ResultJSON = resultJSON
	}
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := h.Journal.Put(entry); err != nil {
		return nil, errf(CodeIndeterminate, "result persist failed after exec: %v", err)
	}
	return &AcceptResult{
		Ack: CommandAck{
			CommandID:            cmd.CommandID,
			Status:               entry.Status,
			ConnectionGeneration: h.ConnectionGeneration,
			JournaledAt:          entry.JournaledAt,
		},
		Entry:    entry,
		Executed: true,
	}, nil
}

// Cancel 处理 Manage 下发的 tool.cancel：journal 标记 + 取消运行中 ctx。
func (h *CommandHandler) Cancel(commandID, workgroupID string) (*AcceptResult, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return nil, errf(CodeSchemaMismatch, "command_id required")
	}
	existing, err := h.Journal.Get(commandID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if existing == nil {
		entry := JournalEntry{
			CommandID:   commandID,
			WorkgroupID: workgroupID,
			Status:      "canceled",
			ErrorCode:   string(CodeCanceled),
			JournaledAt: now,
			UpdatedAt:   now,
		}
		if err := h.Journal.Put(entry); err != nil {
			return nil, err
		}
		h.invokeRunningCancel(commandID)
		return &AcceptResult{
			Ack: CommandAck{
				CommandID:            commandID,
				Status:               "canceled",
				ConnectionGeneration: h.ConnectionGeneration,
				JournaledAt:          now,
			},
			Entry: entry,
		}, nil
	}
	switch existing.Status {
	case "succeeded", "failed", "indeterminate", "rejected", "canceled":
		h.invokeRunningCancel(commandID)
		return &AcceptResult{
			Ack: CommandAck{
				CommandID:            existing.CommandID,
				Status:               existing.Status,
				ConnectionGeneration: h.ConnectionGeneration,
				JournaledAt:          existing.JournaledAt,
			},
			Entry: *existing,
		}, nil
	}
	existing.Status = "canceled"
	existing.ErrorCode = string(CodeCanceled)
	existing.UpdatedAt = now
	if workgroupID != "" && existing.WorkgroupID == "" {
		existing.WorkgroupID = workgroupID
	}
	if err := h.Journal.Put(*existing); err != nil {
		return nil, err
	}
	h.invokeRunningCancel(commandID)
	return &AcceptResult{
		Ack: CommandAck{
			CommandID:            existing.CommandID,
			Status:               existing.Status,
			ConnectionGeneration: h.ConnectionGeneration,
			JournaledAt:          existing.JournaledAt,
		},
		Entry: *existing,
	}, nil
}

func (h *CommandHandler) trackRunning(commandID string, cancel context.CancelFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running == nil {
		h.running = map[string]context.CancelFunc{}
	}
	h.running[commandID] = cancel
}

func (h *CommandHandler) untrackRunning(commandID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running == nil {
		return
	}
	delete(h.running, commandID)
}

func (h *CommandHandler) invokeRunningCancel(commandID string) {
	h.mu.Lock()
	cancel := h.running[commandID]
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (h *CommandHandler) isTrackedRunning(commandID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running == nil {
		return false
	}
	_, ok := h.running[commandID]
	return ok
}

func (h *CommandHandler) reject(cmd ToolCommand, ferr error) (*AcceptResult, error) {
	code := CodeConflict
	msg := ferr.Error()
	if we, ok := ferr.(*Error); ok {
		code = we.Code
		msg = we.Message
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entry := JournalEntry{
		CommandID:   cmd.CommandID,
		PayloadHash: cmd.PayloadHash,
		Status:      "rejected",
		MemberID:    cmd.MemberID,
		WorkgroupID: cmd.WorkgroupID,
		ToolName:    cmd.ToolName,
		ErrorCode:   string(code),
		JournaledAt: now,
		UpdatedAt:   now,
	}
	_ = h.Journal.Put(entry)
	return &AcceptResult{
		Ack: CommandAck{
			CommandID:            cmd.CommandID,
			Status:               "rejected",
			ConnectionGeneration: h.ConnectionGeneration,
			JournaledAt:          now,
		},
		Entry:     entry,
		Rejected:  true,
		ErrorCode: code,
	}, errf(code, "%s", msg)
}

// ApplyArchiveTombstone 提升 lease 栅栏并归档绑定。
func (h *CommandHandler) ApplyArchiveTombstone(t ArchiveTombstone, bindings BindingStore) error {
	if h.Tombstones == nil {
		h.Tombstones = map[string]ArchiveTombstone{}
	}
	h.Tombstones[t.WorkgroupID] = t
	list, err := bindings.List()
	if err != nil {
		return err
	}
	for _, b := range list {
		if b.WorkgroupID != t.WorkgroupID {
			continue
		}
		b.Status = "archived"
		b.LeaseEpoch = t.LeaseEpochAtArchive
		if err := bindings.Put(b); err != nil {
			return err
		}
	}
	return nil
}
