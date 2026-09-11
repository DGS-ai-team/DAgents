package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

const dreamingPrompt = "请整理本次工作经验。你的最终摘要会被系统持久化为长期经验，请只写已由当前上下文或当前工具结果证实的内容，不要写假设。当前工具名称、当前职责和系统传入的手册根目录是唯一有效依据；历史消息中的工具名称、路径和规则可能已经退役，必须用当前工具重新验证，未验证的路径不得记忆。淘汰过时规则，必要的细节才写入当前手册，并在经验中使用已验证的相对路径索引。最后输出简短、可复用的经验摘要。"

func buildDreamingPrompt(base string, promptContext session.DreamingPromptContext) string {
	return fmt.Sprintf("%s\n\n当前职责以本次请求的 system prompt 为准，不在此处重复或改写。当前手册根目录（系统绑定）：%s\n当前可用工具名称（以运行时实际提供为准）：%s", base, promptContext.HandbookRoot, strings.Join(promptContext.Tools, ", "))
}

type DreamingStatus struct {
	State       string    `json:"state"`
	NextAt      time.Time `json:"next_at,omitempty"`
	LastSuccess time.Time `json:"last_success,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

func (s DreamingStatus) MarshalJSON() ([]byte, error) {
	value := map[string]any{"state": s.State}
	if !s.NextAt.IsZero() {
		value["next_at"] = s.NextAt
	}
	if !s.LastSuccess.IsZero() {
		value["last_success"] = s.LastSuccess
	}
	if s.LastError != "" {
		value["last_error"] = s.LastError
	}
	return json.Marshal(value)
}

// DreamingScheduler owns only the daily dreaming loop. Wake triggers and
// their interval do not participate in its due calculation.
type DreamingScheduler struct {
	autonomy *autonomy.Store
	agents   *store.AgentStore
	sessions *session.Manager
	ensure   func(context.Context, string) error
	now      func() time.Time
	interval time.Duration

	mu      sync.Mutex
	tickMu  sync.Mutex
	status  map[string]DreamingStatus
	nextTry map[string]time.Time
	cancel  context.CancelFunc
	started bool
	wg      sync.WaitGroup
}

func NewDreamingScheduler(a *autonomy.Store, agents *store.AgentStore, sessions *session.Manager, ensure func(context.Context, string) error) *DreamingScheduler {
	if ensure == nil && sessions != nil {
		ensure = func(context.Context, string) error { return nil }
	}
	return &DreamingScheduler{autonomy: a, agents: agents, sessions: sessions, ensure: ensure, now: time.Now, interval: time.Minute, status: map[string]DreamingStatus{}, nextTry: map[string]time.Time{}}
}

func (d *DreamingScheduler) Start(ctx context.Context) {
	if d == nil || ctx == nil {
		return
	}
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.started = true
	interval := d.interval
	if interval <= 0 {
		interval = time.Minute
	}
	d.wg.Add(1)
	d.mu.Unlock()
	go func() {
		defer d.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C:
				_ = d.Tick(loopCtx)
			}
		}
	}()
}

func (d *DreamingScheduler) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	cancel := d.cancel
	d.mu.Unlock()
	if cancel != nil {
		cancel()
		d.wg.Wait()
	}
}

func (d *DreamingScheduler) Status(agentID string) DreamingStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status[strings.TrimSpace(agentID)]
}

func (d *DreamingScheduler) CurrentStatus(agentID string, now time.Time) (result DreamingStatus) {
	agentID = strings.TrimSpace(agentID)
	// LastSuccess is durable history, independent of today's scheduling state.
	// Attach it to every return below so waiting, failure, recovery, and disabled
	// projections cannot accidentally erase a prior successful run.
	defer func() {
		if result.LastSuccess.IsZero() {
			if latest, ok := d.autonomy.LatestDreamingCommit(agentID); ok {
				result.LastSuccess = latest.UpdatedAt
			}
		}
	}()
	if d.sessions != nil {
		if attempt, found, err := d.sessions.GetDreamingAttempt(agentID); err == nil && found {
			switch attempt.State {
			case session.DreamingAttemptWaiting:
				// Waiting for an approval is distinct from waiting for today's
				// scheduled time: omit NextAt so clients do not display a stale
				// schedule while a live turn is paused.
				return DreamingStatus{State: "waiting"}
			case session.DreamingAttemptRunning, session.DreamingAttemptCompleted:
				// A completed attempt still needs the durable commit/reset
				// handoff; report it as active until that handoff is acknowledged.
				return DreamingStatus{State: "running"}
			}
		}
	}
	d.mu.Lock()
	status, exists := d.status[agentID]
	d.mu.Unlock()
	if exists && status.State == "running" {
		return status
	}
	profile, configured := d.autonomy.GetProfile(agentID)
	if pending := d.autonomy.ListPendingDreamingCommits(agentID); len(pending) > 0 {
		return DreamingStatus{State: "recovery_pending"}
	}
	if !configured || !profile.DreamingEnabled {
		// Disabling dreaming stops future work but does not erase the durable
		// history of the last successful run.
		return DreamingStatus{State: "disabled"}
	}
	if exists && status.State == "failed" {
		return status
	}
	location, err := time.LoadLocation(profile.Timezone)
	if err != nil {
		return DreamingStatus{State: "failed", LastError: err.Error()}
	}
	localNow := now.UTC().In(location)
	date := localNow.Format("2006-01-02")
	if commit, ok := d.autonomy.GetDreamingCommit(agentID, date); ok && commit.ResetApplied {
		return DreamingStatus{State: "succeeded", LastSuccess: commit.UpdatedAt, NextAt: dueForNextDay(localNow, location, profile.DreamingTime)}
	}
	due, err := time.ParseInLocation("2006-01-02 15:04", date+" "+profile.DreamingTime, location)
	if err != nil {
		return DreamingStatus{State: "failed", LastError: err.Error()}
	}
	return DreamingStatus{State: "waiting", NextAt: due}
}

func (s *Server) registerDreamingRoutes() {
	s.mux.HandleFunc("GET /v1/agents/{agent_id}/dreaming", s.handleDreamingStatus)
}

func (s *Server) handleDreamingStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.dreamingSched == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "dreaming_unavailable", "dreaming scheduler unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.dreamingSched.CurrentStatus(id, time.Now()))
}

func (d *DreamingScheduler) Tick(ctx context.Context) error {
	if d == nil || d.autonomy == nil || d.agents == nil || d.sessions == nil || d.ensure == nil {
		return fmt.Errorf("dreaming scheduler unavailable")
	}
	d.tickMu.Lock()
	defer d.tickMu.Unlock()
	now := d.now().UTC()
	records, err := d.agents.List(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, rec := range records {
		if rec.Archived || strings.TrimSpace(rec.AgentID) == "" {
			continue
		}
		snapshot, parseErr := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
		if parseErr != nil || snapshot.AgentType != "auto" {
			continue
		}
		if err := d.tickAgent(ctx, rec.AgentID, now); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (d *DreamingScheduler) tickAgent(ctx context.Context, agentID string, now time.Time) error {
	agentID = strings.TrimSpace(agentID)
	if until := d.retryAt(agentID); !until.IsZero() && now.Before(until) {
		return nil
	}
	// Recover the session-owned attempt before looking at today's schedule.
	// An attempt is authoritative for its own date/revision and must never be
	// replaced by a fresh round while it is still waiting or running.
	if err := d.ensure(ctx, agentID); err != nil {
		return d.failed(agentID, now, err)
	}
	attempt, hasAttempt, err := d.sessions.GetDreamingAttempt(agentID)
	if err != nil {
		return d.failed(agentID, now, err)
	}
	if hasAttempt {
		switch attempt.State {
		case session.DreamingAttemptWaiting:
			d.setStatus(agentID, DreamingStatus{State: "waiting"})
			return nil
		case session.DreamingAttemptRunning:
			d.setStatus(agentID, DreamingStatus{State: "recovery_pending"})
			return nil
		case session.DreamingAttemptCompleted:
			return d.finalizeAttempt(ctx, agentID, attempt, now)
		case session.DreamingAttemptFailed, session.DreamingAttemptCancelled:
			// A failed/cancelled attempt is terminal. Preserve it for audit, but
			// allow a later scheduled round to create a new attempt.
		}
	}
	profile, configured := d.autonomy.GetProfile(agentID)
	pending := d.autonomy.ListPendingDreamingCommits(agentID)
	if len(pending) > 0 {
		if err := d.ensure(ctx, agentID); err != nil {
			return d.failed(agentID, now, err)
		}
		for _, commit := range pending {
			acquired, err := d.recoverPending(ctx, agentID, commit)
			if err != nil {
				return d.failed(agentID, now, err)
			}
			if !acquired {
				d.setStatus(agentID, DreamingStatus{State: "waiting"})
				return nil
			}
		}
		if len(d.autonomy.ListPendingDreamingCommits(agentID)) > 0 {
			d.setStatus(agentID, DreamingStatus{State: "recovery_pending"})
			return nil
		}
	}
	if !configured || !profile.DreamingEnabled {
		d.setStatus(agentID, DreamingStatus{State: "disabled"})
		return nil
	}
	location, err := time.LoadLocation(profile.Timezone)
	if err != nil {
		return d.failed(agentID, now, err)
	}
	localNow := now.In(location)
	date := localNow.Format("2006-01-02")
	due, err := time.ParseInLocation("2006-01-02 15:04", date+" "+profile.DreamingTime, location)
	if err != nil {
		return d.failed(agentID, now, err)
	}
	if commit, ok := d.autonomy.GetDreamingCommit(agentID, date); ok && commit.ResetApplied {
		d.setStatus(agentID, DreamingStatus{State: "succeeded", LastSuccess: commit.UpdatedAt, NextAt: dueForNextDay(localNow, location, profile.DreamingTime)})
		return nil
	}
	if localNow.Before(due) {
		d.setStatus(agentID, DreamingStatus{State: "waiting", NextAt: due})
		return nil
	}
	leaseCtx, release, acquired, err := d.sessions.TryAcquireMaintenanceContext(ctx, agentID)
	if err != nil {
		return d.failed(agentID, now, err)
	}
	if !acquired {
		d.setStatus(agentID, DreamingStatus{State: "waiting"})
		return nil
	}
	defer release()
	d.setStatus(agentID, DreamingStatus{State: "running"})
	experience, _ := d.autonomy.GetExperience(agentID)
	promptContext, contextErr := d.sessions.GetDreamingPromptContext(agentID)
	if contextErr != nil {
		return d.failed(agentID, now, contextErr)
	}
	prompt := buildDreamingPrompt(dreamingPrompt, promptContext)
	_, err = d.sessions.RunDreaming(leaseCtx, agentID, prompt, profile.MaxToolRounds, session.DreamingMetadata{
		LocalDate: date, ExperienceRevision: experience.Revision,
	})
	if err != nil {
		if attempt, ok, getErr := d.sessions.GetDreamingAttempt(agentID); getErr == nil && ok && attempt.State == session.DreamingAttemptWaiting {
			d.setStatus(agentID, DreamingStatus{State: "waiting"})
			return nil
		}
		return d.failed(agentID, now, err)
	}
	attempt, ok, err := d.sessions.GetDreamingAttempt(agentID)
	if err != nil || !ok || attempt.State != session.DreamingAttemptCompleted {
		if err == nil {
			err = fmt.Errorf("dreaming attempt did not complete")
		}
		return d.failed(agentID, now, err)
	}
	if err := d.finalizeAttemptWithLease(leaseCtx, agentID, attempt, now); err != nil {
		return err
	}
	d.mu.Lock()
	d.status[agentID] = DreamingStatus{State: "succeeded", LastSuccess: now, NextAt: dueForNextDay(localNow, location, profile.DreamingTime)}
	delete(d.nextTry, agentID)
	d.mu.Unlock()
	return nil
}

func (d *DreamingScheduler) finalizeAttempt(ctx context.Context, agentID string, attempt session.DreamingAttempt, now time.Time) error {
	leaseCtx, release, acquired, err := d.sessions.TryAcquireMaintenanceContext(ctx, agentID)
	if err != nil {
		return d.failed(agentID, now, err)
	}
	if !acquired {
		d.setStatus(agentID, DreamingStatus{State: "waiting"})
		return nil
	}
	defer release()
	return d.finalizeAttemptWithLease(leaseCtx, agentID, attempt, now)
}

func (d *DreamingScheduler) finalizeAttemptWithLease(ctx context.Context, agentID string, attempt session.DreamingAttempt, now time.Time) error {
	if attempt.AgentID != agentID || attempt.SessionID == "" || attempt.TurnID == "" || attempt.LocalDate == "" {
		return d.failed(agentID, now, fmt.Errorf("invalid dreaming attempt identity"))
	}
	resetID := fmt.Sprintf("dreaming:%s:%s", agentID, attempt.LocalDate)
	if _, err := d.autonomy.CommitDreaming(autonomy.DreamingCommitInput{AgentID: agentID, LocalDate: attempt.LocalDate, CommitID: resetID, SessionID: attempt.SessionID, Boundary: attempt.Boundary, ExpectedRevision: attempt.ExperienceRevision, Content: attempt.FinalMessage, Now: now}); err != nil {
		return d.failed(agentID, now, err)
	}
	if _, err := d.sessions.ResetActiveContext(ctx, attempt.SessionID, attempt.Boundary, resetID); err != nil {
		return d.failed(agentID, now, err)
	}
	if _, err := d.autonomy.MarkDreamingResetApplied(agentID, attempt.LocalDate, resetID); err != nil {
		return d.failed(agentID, now, err)
	}
	if err := d.sessions.AckDreamingAttempt(ctx, attempt.SessionID, attempt.TurnID); err != nil {
		return d.failed(agentID, now, err)
	}
	d.setStatus(agentID, DreamingStatus{State: "succeeded", LastSuccess: now})
	return nil
}

func (d *DreamingScheduler) recoverPending(ctx context.Context, agentID string, commit autonomy.DreamingCommit) (bool, error) {
	leaseCtx, release, acquired, err := d.sessions.TryAcquireMaintenanceContext(ctx, agentID)
	if err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	defer release()
	if _, err := d.sessions.ResetActiveContext(leaseCtx, commit.SessionID, commit.Boundary, commit.CommitID); err != nil {
		return true, err
	}
	_, err = d.autonomy.MarkDreamingResetApplied(agentID, commit.LocalDate, commit.CommitID)
	return true, err
}

func dueForNextDay(localNow time.Time, location *time.Location, dreamingTime string) time.Time {
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, location)
	due, err := time.ParseInLocation("2006-01-02 15:04", next.Format("2006-01-02")+" "+dreamingTime, location)
	if err != nil {
		return next
	}
	return due
}

func (d *DreamingScheduler) retryAt(agentID string) time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.nextTry[agentID]
}

func (d *DreamingScheduler) failed(agentID string, now time.Time, err error) error {
	d.mu.Lock()
	d.status[agentID] = DreamingStatus{State: "failed", LastError: err.Error(), NextAt: now.Add(5 * time.Minute)}
	d.nextTry[agentID] = now.Add(5 * time.Minute)
	d.mu.Unlock()
	return err
}

func (d *DreamingScheduler) setStatus(agentID string, status DreamingStatus) {
	d.mu.Lock()
	d.status[agentID] = status
	d.mu.Unlock()
}
