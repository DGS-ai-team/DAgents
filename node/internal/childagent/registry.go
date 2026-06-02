package childagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// RestrictedRegistry 仅暴露 allowed 工具子集，委托 *tools.Registry 执行。
type RestrictedRegistry struct {
	inner   *tools.Registry
	allowed map[string]struct{}
}

// NewRestrictedRegistry 构造子 Agent 工具表；allowed 为空则拒绝全部 Execute。
func NewRestrictedRegistry(inner *tools.Registry, allowed []string) *RestrictedRegistry {
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		n := strings.TrimSpace(name)
		if n == "" || IsParentOnlyTool(n) {
			continue
		}
		set[n] = struct{}{}
	}
	return &RestrictedRegistry{inner: inner, allowed: set}
}

// Definitions 返回过滤后的 OpenAI tools 列表。
func (r *RestrictedRegistry) Definitions() []tools.ToolDef {
	if r.inner == nil {
		return nil
	}
	all := r.inner.Definitions()
	out := make([]tools.ToolDef, 0, len(r.allowed))
	for _, def := range all {
		name := def.Function.Name
		if _, ok := r.allowed[name]; ok {
			out = append(out, def)
		}
	}
	return out
}

// Execute 执行允许的工具；否则返回 error 文本。
func (r *RestrictedRegistry) Execute(ctx context.Context, name, arguments string) (string, error) {
	if err := r.check(name); err != nil {
		return "", err
	}
	return r.inner.Execute(ctx, name, arguments)
}

// StartBackground 委托底层 registry 的后台执行。
func (r *RestrictedRegistry) StartBackground(ctx context.Context, sessionID, toolName, toolCallID, cleanedArgs string) (string, error) {
	if err := r.check(toolName); err != nil {
		return "", err
	}
	return r.inner.StartBackground(ctx, sessionID, toolName, toolCallID, cleanedArgs)
}

func (r *RestrictedRegistry) check(name string) error {
	if IsParentOnlyTool(name) {
		return fmt.Errorf("tool %q is not allowed for child agent", name)
	}
	if _, ok := r.allowed[name]; !ok {
		return fmt.Errorf("tool %q is not in child allowed_tools", name)
	}
	return nil
}

var _ tools.Executor = (*RestrictedRegistry)(nil)
