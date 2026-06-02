package triggers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/google/uuid"
)

// MessageSubmitter 将渲染后的 trigger 任务投递到 session 队列。
type MessageSubmitter interface {
	EnsureSession(requestedID string) (sessionID string, err error)
	SubmitTriggerMessage(sessionID, triggerID, content string) error
}

// Scheduler 轮询到期触发器并统一 fire 入口。
type Scheduler struct {
	store        *Store
	submitter    MessageSubmitter
	cmdGate      CmdGate
	pollInterval time.Duration
	logger       *slog.Logger

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewScheduler 构造调度器；pollSeconds 至少 1 秒。
func NewScheduler(store *Store, submitter MessageSubmitter, pollSeconds int) *Scheduler {
	if pollSeconds < 1 {
		pollSeconds = 5
	}
	return &Scheduler{
		store:        store,
		submitter:    submitter,
		cmdGate:      NewShellCmdGate(),
		pollInterval: time.Duration(pollSeconds) * time.Second,
		logger:       logx.Discard(),
	}
}

// SetLogger 注入结构化日志；nil 时丢弃输出（单测默认）。
func (s *Scheduler) SetLogger(logger *slog.Logger) {
	if s == nil {
		return
	}
	s.logger = discardLogger(logger)
}

// SetCmdGate 注入 cmd 门控执行器（测试用）。
func (s *Scheduler) SetCmdGate(gate CmdGate) {
	if gate != nil {
		s.cmdGate = gate
	}
}

// Start 启动后台轮询；已在运行则幂等忽略。
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopCh != nil {
		return
	}
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.logger.Info("trigger scheduler started", "poll_seconds", int(s.pollInterval.Seconds()))
	go s.runLoop()
}

// Stop 停止轮询并等待 goroutine 退出。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopCh == nil {
		s.mu.Unlock()
		return
	}
	close(s.stopCh)
	done := s.doneCh
	s.stopCh = nil
	s.doneCh = nil
	s.mu.Unlock()
	<-done
}

// FireTrigger 手动或工具触发指定触发器。

// 关键分支：不存在时返回 errTriggerNotFound；手动 fire 不执行 schedule cmd 门控。
func (s *Scheduler) FireTrigger(triggerID, reason string, payload map[string]any, force bool) (FireRecord, error) {
	def, ok := s.store.GetTrigger(triggerID)
	if !ok {
		return FireRecord{}, errTriggerNotFound
	}
	return s.fire(*def, reason, payload, force), nil
}

func (s *Scheduler) runLoop() {
	defer close(s.doneCh)
	s.tickDue(time.Now())
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.tickDue(now)
		}
	}
}

func (s *Scheduler) tickDue(now time.Time) {
	for _, def := range s.store.ListEnabledTriggers() {
		decision, updated := EvaluateDue(def, now)
		switch decision {
		case DueAdvanceOnly:
			_ = s.store.ReplaceTrigger(updated)
			s.logger.Debug("trigger schedule advanced only",
				"trigger_id", def.TriggerID,
				"name", def.Name,
				"next_fire_at", updated.NextFireAt,
			)
		case DueFire:
			s.fire(def, "schedule", map[string]any{}, false)
		default:
		}
	}
}

func (s *Scheduler) fire(def Definition, reason string, payload map[string]any, force bool) FireRecord {
	if payload == nil {
		payload = map[string]any{}
	}
	if !def.Enabled && !force {
		record := s.record(def, FireStatusSkipped, reason, payload, "trigger is disabled", nil, nil, "")
		s.logFireRecord(record)
		return record
	}
	if s.store.HasPendingDelivery(def.TriggerID) {
		record := FireRecord{
			TriggerID: def.TriggerID,
			Status:    FireStatusSkipped,
			Reason:    reason,
			Message:   "pending delivery not consumed",
			Payload:   payload,
			FiredAt:   timeToUnixFloat(time.Now()),
		}
		if reason != "schedule" {
			record = s.store.AddHistory(record)
		}
		s.logFireRecord(record)
		return record
	}
	if reason == "schedule" {
		if cmd := ConditionCmd(def.Condition); cmd != "" {
			ok, detail, err := s.runCmdGate(cmd)
			if err != nil {
				return s.rescheduleAfterCmdSkip(def, reason, payload, "cmd gate error: "+err.Error())
			}
			if !ok {
				msg := "cmd gate rejected"
				if detail != "" {
					msg = msg + ": " + detail
				}
				return s.rescheduleAfterCmdSkip(def, reason, payload, msg)
			}
			payload["cmd_gate"] = detail
		}
	}
	requestedSession := ""
	if def.TargetSessionID != nil {
		requestedSession = *def.TargetSessionID
	}
	sessionID, err := s.submitter.EnsureSession(requestedSession)
	if err != nil {
		record := s.record(def, FireStatusError, reason, payload, err.Error(), nil, nil, "")
		s.logFireRecord(record)
		return record
	}
	clientID := fmt.Sprintf("trigger-%s", def.TriggerID)
	if def.ClientID != nil && *def.ClientID != "" {
		clientID = *def.ClientID
	}
	content := RenderTaskTemplate(def.TaskTemplate, def, reason, payload)
	if err := s.submitter.SubmitTriggerMessage(sessionID, def.TriggerID, content); err != nil {
		record := s.record(def, FireStatusError, reason, payload, err.Error(), &sessionID, &clientID, content)
		s.logFireRecord(record)
		return record
	}
	s.store.MarkPendingDelivery(def.TriggerID)
	if _, err := s.store.MarkFired(def.TriggerID, time.Now()); err != nil {
		record := s.record(def, FireStatusError, reason, payload, err.Error(), &sessionID, &clientID, content)
		s.logFireRecord(record)
		return record
	}
	record := s.record(def, FireStatusQueued, reason, payload, "queued", &sessionID, &clientID, content)
	s.logFireRecord(record)
	return record
}

func (s *Scheduler) runCmdGate(cmd string) (bool, string, error) {
	if s.cmdGate == nil {
		s.cmdGate = NewShellCmdGate()
	}
	return s.cmdGate.Run(cmd)
}

func (s *Scheduler) rescheduleAfterCmdSkip(def Definition, reason string, payload map[string]any, message string) FireRecord {
	updated := def.RescheduleNextFire(time.Now())
	_ = s.store.ReplaceTrigger(updated)
	record := FireRecord{
		FireID:    uuid.NewString(),
		TriggerID: def.TriggerID,
		Status:    FireStatusSkipped,
		Reason:    reason,
		Message:   message,
		Payload:   payload,
		FiredAt:   timeToUnixFloat(time.Now()),
	}
	if reason != "schedule" {
		record = s.store.AddHistory(record)
	}
	s.logFireRecord(record)
	return record
}

func (s *Scheduler) record(
	def Definition,
	status FireStatus,
	reason string,
	payload map[string]any,
	message string,
	sessionID, clientID *string,
	content string,
) FireRecord {
	record := FireRecord{
		FireID:    uuid.NewString(),
		TriggerID: def.TriggerID,
		Status:    status,
		Reason:    reason,
		SessionID: sessionID,
		ClientID:  clientID,
		Content:   content,
		Message:   message,
		Payload:   payload,
		FiredAt:   timeToUnixFloat(time.Now()),
	}
	return s.store.AddHistory(record)
}

// RunOnceForTest 执行一次 due 扫描（单测用）。
func (s *Scheduler) RunOnceForTest(_ context.Context, now time.Time) {
	s.tickDue(now)
}
