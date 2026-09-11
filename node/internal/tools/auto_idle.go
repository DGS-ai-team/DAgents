package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

type trustedAutoIdleActivation struct {
	agentID   string
	triggerID string
	delivery  string
}

type autoIdleActivationKey struct{}

// WithTrustedAutoIdleActivation marks one provider-validated system-auto
// activation. The marker is created only by the session ingress after the
// TriggerToolRoundProvider has accepted the agent/trigger/delivery tuple.
func WithTrustedAutoIdleActivation(ctx context.Context, agentID, triggerID, deliveryID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, autoIdleActivationKey{}, trustedAutoIdleActivation{agentID: agentID, triggerID: triggerID, delivery: deliveryID})
}

func trustedAutoIdle(ctx context.Context) bool {
	v, ok := ctx.Value(autoIdleActivationKey{}).(trustedAutoIdleActivation)
	return ok && v.agentID != "" && v.triggerID != "" && v.delivery != ""
}

// TrustedAutoIdleAvailable reports whether this request belongs to the
// provider-validated system-auto activation that may use the control tool.
func TrustedAutoIdleAvailable(ctx context.Context) bool { return trustedAutoIdle(ctx) }

func TrustedAutoIdleAgentID(ctx context.Context) string {
	v, _ := ctx.Value(autoIdleActivationKey{}).(trustedAutoIdleActivation)
	return v.agentID
}

func autoIdleToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "auto_idle",
		Description: "仅检查并确认没有工作：不执行写入或其他动作；不得在已有完成工作、工具失败或审批后调用。",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
	}}
}

func (r *Registry) DefinitionsForContext(ctx context.Context) []ToolDef {
	defs := r.Definitions()
	if trustedAutoIdle(ctx) {
		defs = append(defs, autoIdleToolDef())
	}
	return defs
}

func (r *Registry) executeAutoIdle(ctx context.Context, args json.RawMessage) (string, error) {
	if !trustedAutoIdle(ctx) {
		return "", fmt.Errorf("auto_idle is only available during a trusted system auto activation")
	}
	if err := ValidateAutoIdleArguments(args); err != nil {
		return "", err
	}
	return `{"no_work":true}`, nil
}

func ValidateAutoIdleArguments(args json.RawMessage) error {
	var fields map[string]json.RawMessage
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, &fields); err != nil {
		return fmt.Errorf("invalid auto_idle arguments")
	}
	if len(fields) != 0 {
		return fmt.Errorf("auto_idle accepts no arguments")
	}
	return nil
}
