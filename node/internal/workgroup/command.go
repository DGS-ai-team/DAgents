package workgroup

import (
	"time"
)

// CommandHandler 接受 tool.command：先 journal 再返回 ack；不重复执行。
type CommandHandler struct {
	Bindings             BindingStore
	Journal              CommandJournal
	ConnectionGeneration int64
	CatalogRevision      string // 当前 Node manifest revision；空则跳过 catalog 检查
	Tombstones           map[string]ArchiveTombstone
	// Executor 可选；nil 时 D2 仅 accept 不执行（仍计 executions=0→1 在 MarkRunning）
	Executor func(cmd ToolCommand) (resultJSON string, err error)
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
		// 已 journal：返回状态，不重执行
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

	// D2 可选同步执行：先标 running，执行一次，落终态
	entry.Status = "running"
	entry.Executions = 1
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = h.Journal.Put(entry)

	resultJSON, execErr := h.Executor(cmd)
	if execErr != nil {
		if we, ok := execErr.(*Error); ok && we.Code == CodeIndeterminate {
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
	ack.Status = entry.Status
	return &AcceptResult{Ack: ack, Entry: entry, Executed: true}, nil
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
