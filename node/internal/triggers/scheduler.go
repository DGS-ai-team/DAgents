package triggers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

// SessionResolver 解析 latest_active 投递目标。
type SessionResolver interface {
	ResolveLatestActiveUserSessionID(ctx context.Context) (string, error)
}

// Scheduler 轮询到期触发器并统一 fire 入口。
type Scheduler struct {
	store           *Store
	submitter       MessageSubmitter
	sessionResolver SessionResolver
	cmdGate         CmdGate
	pollInterval    time.Duration
	logger          *slog.Logger

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

// SetSessionResolver 注入 latest_active 会话解析器。
func (s *Scheduler) SetSessionResolver(resolver SessionResolver) {
	s.sessionResolver = resolver
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
//
// opts 非 nil 时为一次性 override（如 HTTP fire / 审批选项）；nil 时使用 def 持久化配置。
func (s *Scheduler) FireTrigger(triggerID, reason string, payload map[string]any, force bool, opts *FireOptions) (FireRecord, error) {
	def, ok := s.store.GetTrigger(triggerID)
	if !ok {
		return FireRecord{}, errTriggerNotFound
	}
	return s.fire(context.Background(), *def, reason, payload, force, opts), nil
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
			s.fire(context.Background(), def, "schedule", map[string]any{}, false, nil)
		default:
		}
	}
}

func (s *Scheduler) fire(ctx context.Context, def Definition, reason string, payload map[string]any, force bool, opts *FireOptions) FireRecord {
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
	requestedSession, effectiveMode, bindAfterFire, err := s.resolveFireSession(ctx, def, opts)
	if err != nil {
		record := s.record(def, FireStatusError, reason, payload, err.Error(), nil, nil, "")
		s.logFireRecord(record)
		return record
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
	if bindAfterFire {
		s.bindNewSession(def, sessionID, effectiveMode)
	}
	record := s.record(def, FireStatusQueued, reason, payload, "queued", &sessionID, &clientID, content)
	s.logFireRecord(record)
	return record
}

func (s *Scheduler) bindNewSession(def Definition, sessionID string, effectiveMode SessionTargetMode) {
	if effectiveMode != SessionTargetNewSession {
		return
	}
	if hasBoundSessionID(def) {
		return
	}
	updated := def
	updated.SessionTargetMode = SessionTargetFixed
	updated.TargetSessionID = &sessionID
	_ = s.store.ReplaceTrigger(updated)
}

func (s *Scheduler) resolveFireSession(ctx context.Context, def Definition, opts *FireOptions) (requestedSession string, effectiveMode SessionTargetMode, bindAfterFire bool, err error) {
	mode := def.EffectiveSessionTargetMode()
	fixedID := ""
	if def.TargetSessionID != nil {
		fixedID = strings.TrimSpace(*def.TargetSessionID)
	}
	if opts != nil {
		mode = opts.SessionTargetMode
		fixedID = strings.TrimSpace(opts.FixedSessionID)
	}

	switch mode {
	case SessionTargetFixed:
		return fixedID, SessionTargetFixed, false, nil
	case SessionTargetNewSession:
		if fixedID != "" {
			return fixedID, SessionTargetFixed, false, nil
		}
		return "", SessionTargetNewSession, true, nil
	case SessionTargetLatestActive:
		if s.sessionResolver == nil {
			return "", mode, false, fmt.Errorf("session resolver not configured")
		}
		id, err := s.sessionResolver.ResolveLatestActiveUserSessionID(ctx)
		if err != nil {
			return "", mode, false, err
		}
		return id, SessionTargetLatestActive, false, nil
	default:
		return fixedID, SessionTargetFixed, false, nil
	}
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
