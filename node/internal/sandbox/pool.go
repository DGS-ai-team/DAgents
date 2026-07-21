package sandbox

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Pool 跟踪内存中 Agent 的常驻沙箱：加载时 Ensure，空闲回收，卸出时 Release。
type Pool struct {
	idleTimeout time.Duration
	logger      *slog.Logger

	mu      sync.Mutex
	runners map[string]*DockerRunner

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewPool 构造池；idleTimeout≤0 时用 DefaultIdleTimeout。
func NewPool(idleTimeout time.Duration, logger *slog.Logger) *Pool {
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Pool{
		idleTimeout: idleTimeout,
		logger:      logger,
		runners:     make(map[string]*DockerRunner),
		stopCh:      make(chan struct{}),
	}
}

// StartIdleReaper 后台周期检查空闲容器并回收（可重复调用，仅首次生效）。
func (p *Pool) StartIdleReaper(interval time.Duration) {
	if p == nil {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-p.stopCh:
				return
			case now := <-t.C:
				p.reapIdle(now)
			}
		}
	}()
}

// Close 停止回收协程并释放全部容器。
func (p *Pool) Close() {
	if p == nil {
		return
	}
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
	p.wg.Wait()
	p.mu.Lock()
	ids := make([]string, 0, len(p.runners))
	for id := range p.runners {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.Release(id)
	}
}

// Register 登记 runner（不 Ensure）；同 agent 已存在则先 Release 旧实例。
func (p *Pool) Register(runner *DockerRunner) {
	if p == nil || runner == nil {
		return
	}
	id := runner.AgentID
	p.mu.Lock()
	if old, ok := p.runners[id]; ok && old != runner {
		p.mu.Unlock()
		old.Release(context.Background())
		p.mu.Lock()
	}
	if runner.IdleTimeout <= 0 {
		runner.IdleTimeout = p.idleTimeout
	}
	p.runners[id] = runner
	p.mu.Unlock()
}

// Ensure 登记并预创建常驻容器。
func (p *Pool) Ensure(ctx context.Context, runner *DockerRunner) error {
	if p == nil {
		if runner == nil {
			return nil
		}
		return runner.Ensure(ctx)
	}
	p.Register(runner)
	return runner.Ensure(ctx)
}

// Get 返回已登记的 runner。
func (p *Pool) Get(agentID string) *DockerRunner {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runners[agentID]
}

// Release 卸出 Agent 时回收容器并从池中移除。
func (p *Pool) Release(agentID string) {
	if p == nil {
		ReleaseAgent(agentID)
		return
	}
	p.mu.Lock()
	runner := p.runners[agentID]
	delete(p.runners, agentID)
	p.mu.Unlock()
	if runner != nil {
		runner.Release(context.Background())
		p.logger.Info("docker sandbox released", "agent_id", agentID, "container", runner.Name)
		return
	}
	ReleaseAgent(agentID)
}

// RecycleIdleContainer 仅回收空闲容器，runner 仍留在池中（Agent 可能仍在内存）。
func (p *Pool) RecycleIdleContainer(agentID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	runner := p.runners[agentID]
	p.mu.Unlock()
	if runner == nil || !runner.IdleExpired(time.Now()) {
		return
	}
	runner.Release(context.Background())
	p.logger.Info("docker sandbox idle recycled", "agent_id", agentID, "container", runner.Name)
}

func (p *Pool) reapIdle(now time.Time) {
	p.mu.Lock()
	list := make([]*DockerRunner, 0, len(p.runners))
	for _, r := range p.runners {
		list = append(list, r)
	}
	p.mu.Unlock()
	for _, r := range list {
		if r.IdleExpired(now) {
			r.Release(context.Background())
			p.logger.Info("docker sandbox idle recycled", "agent_id", r.AgentID, "container", r.Name)
		}
	}
}
