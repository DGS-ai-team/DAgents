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

// TargetedMessageSubmitter is implemented by Node adapters that can resolve
// an already registered Agent runtime. The legacy interface remains supported
// for embedded callers and tests.
type TargetedMessageSubmitter interface {
	MessageSubmitter
	EnsureSessionForAgent(targetAgentID, requestedID string) (string, error)
}

type DeliveryMessageSubmitter interface {
	SubmitTriggerMessageWithDelivery(sessionID, triggerID, deliveryID, content string) error
}

type ConditionRequest struct {
	TriggerID  string
	DeliveryID string
	SessionID  string
	AgentID    string
	Revision   int64
	Occurrence *float64
	Command    string
}

type ConditionResultStatus string

const (
	ConditionMatched          ConditionResultStatus = "matched"
	ConditionNotMatched       ConditionResultStatus = "not_matched"
	ConditionAwaitingApproval ConditionResultStatus = "awaiting_approval"
)

type ConditionResult struct {
	Status   ConditionResultStatus
	Rejected bool
	Failed   bool
}

// ConditionRunner is an injected, Agent-bound policy/tool execution seam.
// The triggers package never invokes a shell directly.
type ConditionRunner func(context.Context, ConditionRequest) (ConditionResult, error)

type AutoMessageSubmitter interface {
	SubmitAutoTriggerMessage(sessionID, triggerID, deliveryID, content string) error
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
	pollInterval    time.Duration
	logger          *slog.Logger
	conditionRunner ConditionRunner

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

type ConditionCompletion struct {
	TriggerID  string
	DeliveryID string
	SessionID  string
	AgentID    string
	Revision   int64
	Occurrence *float64
	Matched    bool
	Rejected   bool
	Failed     bool
}

// CompleteCondition finishes a persisted condition claim after approval. A
// rejected approval releases the exact claim without submitting the task.
func (s *Scheduler) CompleteCondition(ctx context.Context, completion ConditionCompletion) (FireRecord, error) {
	_ = ctx
	if s == nil || s.store == nil {
		return FireRecord{}, fmt.Errorf("condition scheduler unavailable")
	}
	def, claimed, err := s.store.BeginConditionCompletion(completion)
	if err != nil {
		return FireRecord{}, err
	}
	if !claimed {
		return FireRecord{}, fmt.Errorf("condition completion already claimed")
	}
	if completion.Failed {
		return s.record(def, FireStatusError, def.PendingConditionReason, def.PendingConditionPayload, "condition execution failed", def.PendingSessionID, def.ClientID, def.PendingConditionContent), nil
	}
	if !completion.Matched {
		message := "condition not satisfied"
		if completion.Rejected {
			message = "condition approval rejected"
		}
		return s.record(def, FireStatusSkipped, def.PendingConditionReason, def.PendingConditionPayload, message, def.PendingSessionID, def.ClientID, def.PendingConditionContent), nil
	}
	content := def.PendingConditionContent
	var submitErr error
	if auto, ok := s.submitter.(AutoMessageSubmitter); ok && def.Controller == "auto" {
		submitErr = auto.SubmitAutoTriggerMessage(completion.SessionID, def.TriggerID, completion.DeliveryID, content)
	} else if targeted, ok := s.submitter.(DeliveryMessageSubmitter); ok {
		submitErr = targeted.SubmitTriggerMessageWithDelivery(completion.SessionID, def.TriggerID, completion.DeliveryID, content)
	} else {
		submitErr = s.submitter.SubmitTriggerMessage(completion.SessionID, def.TriggerID, content)
	}
	if submitErr != nil {
		return FireRecord{}, submitErr
	}
	return s.record(def, FireStatusQueued, def.PendingConditionReason, def.PendingConditionPayload, "queued", def.PendingSessionID, def.ClientID, content), nil
}

func (s *Scheduler) SetConditionRunner(runner ConditionRunner) {
	s.mu.Lock()
	s.conditionRunner = runner
	s.mu.Unlock()
}

// NewScheduler 构造调度器；pollSeconds 至少 1 秒。
func NewScheduler(store *Store, submitter MessageSubmitter, pollSeconds int) *Scheduler {
	if pollSeconds < 1 {
		pollSeconds = 5
	}
	return &Scheduler{
		store:        store,
		submitter:    submitter,
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
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	s.stopCh = stopCh
	s.doneCh = doneCh
	s.logger.Info("trigger scheduler started", "poll_seconds", int(s.pollInterval.Seconds()))
	go s.runLoop(stopCh, doneCh)
}

// Stop 停止轮询并等待 goroutine 退出。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopCh == nil {
		s.mu.Unlock()
		return
	}
	stopCh := s.stopCh
	doneCh := s.doneCh
	close(stopCh)
	s.stopCh = nil
	s.doneCh = nil
	s.mu.Unlock()
	<-doneCh
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

func (s *Scheduler) FireAuthorized(p Principal, triggerID string, expected int64, reason string, payload map[string]any, force bool, opts *FireOptions) (FireRecord, error) {
	def, ok := s.store.GetTrigger(triggerID)
	if !ok || !p.canOwn(*def) {
		return FireRecord{}, errTriggerNotFound
	}
	if def.Controller != "user" {
		return FireRecord{}, fmt.Errorf("trigger controller does not permit manual fire")
	}
	if expected > 0 && def.Revision != expected {
		return FireRecord{}, fmt.Errorf("revision conflict")
	}
	if expected == 0 {
		expected = def.Revision
	}
	if opts == nil {
		opts = &FireOptions{}
	}
	copy := *opts
	copy.Principal = &p
	copy.ExpectedRevision = expected
	return s.fire(context.Background(), *def, reason, payload, force, &copy), nil
}

func (s *Scheduler) runLoop(stopCh, doneCh chan struct{}) {
	// Stop clears the fields to allow a later restart. Keep these per-run channel
	// references so clearing the fields cannot make the stop case nil.
	defer close(doneCh)
	s.tickDue(time.Now())
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
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
	if def.Controller != "user" && def.Controller != "auto" {
		record := s.record(def, FireStatusError, reason, payload, "trigger controller is retired or invalid", nil, nil, "")
		s.logFireRecord(record)
		return record
	}
	if !def.Enabled && !force {
		record := s.record(def, FireStatusSkipped, reason, payload, "trigger is disabled", nil, nil, "")
		s.logFireRecord(record)
		return record
	}
	if s.store.HasPendingDelivery(def.TriggerID) || (def.PendingDeliveryID != nil && strings.TrimSpace(*def.PendingDeliveryID) != "") {
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
	requestedSession, effectiveMode, bindAfterFire, err := s.resolveFireSession(ctx, def, opts)
	if err != nil {
		record := s.record(def, FireStatusError, reason, payload, err.Error(), nil, nil, "")
		s.logFireRecord(record)
		return record
	}
	if _, targeted := s.submitter.(TargetedMessageSubmitter); targeted && effectiveMode != SessionTargetFixed {
		record := s.record(def, FireStatusError, reason, payload, "session_target_mode is unsupported for routed triggers; use fixed", nil, nil, "")
		return record
	}
	var sessionID string
	if targeted, ok := s.submitter.(TargetedMessageSubmitter); ok && effectiveMode == SessionTargetFixed {
		sessionID, err = targeted.EnsureSessionForAgent(strings.TrimSpace(def.TargetAgentID), requestedSession)
	} else {
		sessionID, err = s.submitter.EnsureSession(requestedSession)
	}
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
	deliveryID := uuid.NewString()
	var occurrence *float64
	if reason == "schedule" {
		occurrence = def.NextFireAt
	}
	var claimErr error
	if opts != nil && opts.Principal != nil {
		claimErr = s.store.ClaimAuthorized(*opts.Principal, def.TriggerID, opts.ExpectedRevision, deliveryID, sessionID, occurrence)
	} else if reason == "schedule" {
		claimErr = s.store.ClaimDeliveryForOccurrenceRevision(def.TriggerID, def.Revision, deliveryID, sessionID, occurrence)
	} else {
		claimErr = s.store.ClaimDeliveryForOccurrence(def.TriggerID, deliveryID, sessionID, occurrence)
	}
	if err := claimErr; err != nil {
		record := s.record(def, FireStatusError, reason, payload, "delivery claim failed: "+err.Error(), &sessionID, &clientID, content)
		return record
	}
	def.PendingOccurrence = cloneFloatPtr(occurrence)
	if ConditionCmd(def.Condition) != "" {
		if err := s.store.SetPendingCondition(def.TriggerID, deliveryID, reason, content, payload); err != nil {
			return s.record(def, FireStatusError, reason, payload, "condition claim metadata failed: "+err.Error(), &sessionID, &clientID, content)
		}
	}
	// Mirror the durable claim before handing the envelope to the runtime. The
	// consumer may acknowledge synchronously, so marking after Submit races
	// with the identity clear and can resurrect a stale in-memory guard.
	s.store.MarkPendingDelivery(def.TriggerID)
	def.PendingDeliveryID = &deliveryID
	def.PendingSessionID = &sessionID
	// Advance the scheduled occurrence while the durable delivery claim is
	// held. A synchronous consumer may clear the claim before Submit returns;
	// leaving MarkFired until after Submit would let a stale tick claim the same
	// occurrence again.
	if reason == "schedule" {
		if _, err := s.store.MarkFired(def.TriggerID, time.Now()); err != nil {
			record := s.record(def, FireStatusError, reason, payload, err.Error(), &sessionID, &clientID, content)
			s.logFireRecord(record)
			return record
		}
	}
	if command := ConditionCmd(def.Condition); command != "" {
		s.mu.Lock()
		runner := s.conditionRunner
		s.mu.Unlock()
		if runner == nil {
			if err := s.store.clearPendingDeliveryIfMatch(def.TriggerID, deliveryID); err != nil {
				return s.record(def, FireStatusError, reason, payload, "condition cleanup failed: "+err.Error(), &sessionID, &clientID, content)
			}
			return s.record(def, FireStatusError, reason, payload, "condition runner unavailable", &sessionID, &clientID, content)
		}
		var ok bool
		var conditionErr error
		var conditionResult ConditionResult
		conditionResult, conditionErr = runner(ctx, ConditionRequest{TriggerID: def.TriggerID, DeliveryID: deliveryID, SessionID: sessionID, AgentID: def.TargetAgentID, Revision: def.Revision, Occurrence: cloneFloatPtr(def.PendingOccurrence), Command: command})
		ok = conditionResult.Status == ConditionMatched
		if conditionResult.Status == ConditionAwaitingApproval && conditionErr == nil {
			record := s.record(def, FireStatusAwaitingApproval, reason, payload, "condition awaiting approval", &sessionID, &clientID, content)
			return record
		}
		if conditionErr != nil || conditionResult.Failed || !ok {
			if cleanupErr := s.store.clearPendingDeliveryIfMatch(def.TriggerID, deliveryID); cleanupErr != nil {
				return s.record(def, FireStatusError, reason, payload, "condition cleanup failed: "+cleanupErr.Error(), &sessionID, &clientID, content)
			}
			message, status := "condition not satisfied", FireStatusSkipped
			if conditionErr != nil {
				status, message = FireStatusError, "condition failed: "+conditionErr.Error()
			} else if conditionResult.Failed {
				status, message = FireStatusError, "condition execution failed"
			}
			return s.record(def, status, reason, payload, message, &sessionID, &clientID, content)
		}
	}
	var submitErr error
	if def.Controller == "auto" {
		if auto, ok := s.submitter.(AutoMessageSubmitter); ok {
			submitErr = auto.SubmitAutoTriggerMessage(sessionID, def.TriggerID, deliveryID, content)
		} else if targeted, ok := s.submitter.(DeliveryMessageSubmitter); ok {
			submitErr = targeted.SubmitTriggerMessageWithDelivery(sessionID, def.TriggerID, deliveryID, content)
		} else {
			submitErr = s.submitter.SubmitTriggerMessage(sessionID, def.TriggerID, content)
		}
	} else if targeted, ok := s.submitter.(DeliveryMessageSubmitter); ok {
		submitErr = targeted.SubmitTriggerMessageWithDelivery(sessionID, def.TriggerID, deliveryID, content)
	} else {
		submitErr = s.submitter.SubmitTriggerMessage(sessionID, def.TriggerID, content)
	}
	if err := submitErr; err != nil {
		record := s.record(def, FireStatusError, reason, payload, err.Error(), &sessionID, &clientID, content)
		s.logFireRecord(record)
		return record
	}
	if reason != "schedule" {
		if _, err := s.store.MarkFired(def.TriggerID, time.Now()); err != nil {
			record := s.record(def, FireStatusError, reason, payload, err.Error(), &sessionID, &clientID, content)
			s.logFireRecord(record)
			return record
		}
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
	mode := def.SessionTargetMode
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
		DeliveryID: strings.TrimSpace(func() string {
			if def.PendingDeliveryID != nil {
				return *def.PendingDeliveryID
			}
			return ""
		}()),
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
