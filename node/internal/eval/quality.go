// Package eval contains deterministic, end-to-end Agent quality evaluation
// primitives. It deliberately depends on neither the model provider nor the
// session runtime: adapters can translate a real or fake Turn trace into the
// small Trace type below, while the scenario assertions remain stable.
package eval

import (
	"fmt"
	"sort"
	"strings"
)

// CheckKind identifies a deterministic scenario assertion.
type CheckKind string

const (
	CheckToolCalled          CheckKind = "tool_called"
	CheckToolCallOrder       CheckKind = "tool_call_order"
	CheckFinalContains       CheckKind = "final_contains"
	CheckFinalNotContains    CheckKind = "final_not_contains"
	CheckResultContains      CheckKind = "result_contains"
	CheckNoToolsAfterCancel  CheckKind = "no_tools_after_cancel"
	CheckVerifiedCompletion  CheckKind = "verified_completion"
	CheckExplicitEmptyOutput CheckKind = "explicit_empty_output"
)

// Criterion is a machine-readable expectation for one golden scenario.
// Value is interpreted according to Kind; Description is kept in reports so
// a failed assertion is actionable without reading the scenario source.
type Criterion struct {
	ID          string
	Kind        CheckKind
	Value       string
	Description string
}

// Scenario is an end-to-end quality case. UserInput is intentionally kept in
// the catalogue so a future real-model adapter can execute the same cases.
type Scenario struct {
	ID        string
	Name      string
	Category  string
	UserInput string
	Criteria  []Criterion
}

// ToolCall is the normalized part of an assistant tool call needed by the
// baseline evaluator. Arguments are retained for future schema assertions.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult is the normalized tool fact observed by the evaluator.
type ToolResult struct {
	ToolCallID  string
	Name        string
	Text        string
	Status      string
	ExitCode    *int
	HasEvidence bool
}

// Trace is the provider-neutral projection of one completed or interrupted
// Turn. Runtime adapters should populate it from authoritative lifecycle and
// tool facts rather than scraping UI text.
type Trace struct {
	ScenarioID           string
	FinalText            string
	ToolCalls            []ToolCall
	ToolResults          []ToolResult
	Steps                int
	Retries              int
	DurationMS           int64
	InputTokens          int
	OutputTokens         int
	Cost                 float64
	CacheHit             bool
	CacheObserved        bool
	Cancelled            bool
	ToolCallsAfterCancel int
	CompletionStatus     string
	VerificationStatus   string
}

// CriterionResult is the result of one scenario assertion.
type CriterionResult struct {
	Criterion Criterion
	Passed    bool
	Detail    string
}

// ScenarioResult is the complete deterministic result for one scenario.
type ScenarioResult struct {
	Scenario   Scenario
	Trace      Trace
	Passed     bool
	Assertions []CriterionResult
}

// Scorecard is the baseline report. Rates are in the range [0, 1].
type Scorecard struct {
	ScenarioCount          int
	ScenarioPassCount      int
	CriterionCount         int
	CriterionPassCount     int
	TaskSuccessRate        float64
	CriterionPassRate      float64
	VerificationCoverage   float64
	CancellationViolations int
	TotalSteps             int
	TotalToolCalls         int
	TotalToolFailures      int
	TotalRetries           int
	TotalDurationMS        int64
	TotalInputTokens       int
	TotalOutputTokens      int
	TotalCost              float64
	CacheObservedCount     int
	CacheHitCount          int
	Results                []ScenarioResult
}

// EvaluateScenario evaluates a trace without model-dependent judging.
func EvaluateScenario(scenario Scenario, trace Trace) ScenarioResult {
	result := ScenarioResult{Scenario: scenario, Trace: trace, Passed: true}
	for _, criterion := range scenario.Criteria {
		passed, detail := evaluateCriterion(criterion, trace)
		result.Assertions = append(result.Assertions, CriterionResult{
			Criterion: criterion,
			Passed:    passed,
			Detail:    detail,
		})
		if !passed {
			result.Passed = false
		}
	}
	return result
}

// EvaluateSuite evaluates the supplied traces against the scenario catalogue.
// Missing traces are explicit failures rather than silently omitted data.
func EvaluateSuite(scenarios []Scenario, traces map[string]Trace) Scorecard {
	score := Scorecard{ScenarioCount: len(scenarios)}
	for _, scenario := range scenarios {
		trace, ok := traces[scenario.ID]
		if !ok {
			trace = Trace{ScenarioID: scenario.ID}
		}
		result := EvaluateScenario(scenario, trace)
		score.Results = append(score.Results, result)
		if result.Passed {
			score.ScenarioPassCount++
		}
		score.TotalSteps += trace.Steps
		score.TotalToolCalls += len(trace.ToolCalls)
		score.TotalRetries += trace.Retries
		score.TotalDurationMS += trace.DurationMS
		score.TotalInputTokens += trace.InputTokens
		score.TotalOutputTokens += trace.OutputTokens
		score.TotalCost += trace.Cost
		if trace.CacheObserved {
			score.CacheObservedCount++
			if trace.CacheHit {
				score.CacheHitCount++
			}
		}
		for _, toolResult := range trace.ToolResults {
			if strings.EqualFold(strings.TrimSpace(toolResult.Status), "failed") || (toolResult.ExitCode != nil && *toolResult.ExitCode != 0) {
				score.TotalToolFailures++
			}
		}
		if strings.TrimSpace(trace.VerificationStatus) != "" {
			score.VerificationCoverage++
		}
		for _, assertion := range result.Assertions {
			score.CriterionCount++
			if assertion.Passed {
				score.CriterionPassCount++
			}
		}
		if trace.Cancelled && trace.ToolCallsAfterCancel > 0 {
			score.CancellationViolations++
		}
	}
	if score.ScenarioCount > 0 {
		score.TaskSuccessRate = float64(score.ScenarioPassCount) / float64(score.ScenarioCount)
	}
	if score.CriterionCount > 0 {
		score.CriterionPassRate = float64(score.CriterionPassCount) / float64(score.CriterionCount)
	}
	if score.ScenarioCount > 0 {
		score.VerificationCoverage /= float64(score.ScenarioCount)
	}
	return score
}

func evaluateCriterion(criterion Criterion, trace Trace) (bool, string) {
	value := strings.TrimSpace(criterion.Value)
	switch criterion.Kind {
	case CheckToolCalled:
		for _, call := range trace.ToolCalls {
			if call.Name == value {
				return true, fmt.Sprintf("tool %q was called", value)
			}
		}
		return false, fmt.Sprintf("tool %q was not called", value)
	case CheckToolCallOrder:
		parts := strings.Split(value, ">")
		positions := make([]int, 0, len(parts))
		for _, want := range parts {
			position := -1
			for i, call := range trace.ToolCalls {
				if call.Name == strings.TrimSpace(want) {
					position = i
					break
				}
			}
			if position < 0 {
				return false, fmt.Sprintf("tool %q is missing", strings.TrimSpace(want))
			}
			positions = append(positions, position)
		}
		for i := 1; i < len(positions); i++ {
			if positions[i-1] >= positions[i] {
				return false, fmt.Sprintf("tool order %q is not satisfied", value)
			}
		}
		return true, fmt.Sprintf("tool order %q is satisfied", value)
	case CheckFinalContains:
		if strings.Contains(trace.FinalText, value) {
			return true, "final answer contains expected evidence"
		}
		return false, fmt.Sprintf("final answer does not contain %q", value)
	case CheckFinalNotContains:
		if !strings.Contains(trace.FinalText, value) {
			return true, "final answer avoids forbidden claim"
		}
		return false, fmt.Sprintf("final answer contains forbidden text %q", value)
	case CheckResultContains:
		for _, result := range trace.ToolResults {
			if strings.Contains(result.Text, value) {
				return true, "tool result contains expected fact"
			}
		}
		return false, fmt.Sprintf("no tool result contains %q", value)
	case CheckNoToolsAfterCancel:
		if trace.Cancelled && trace.ToolCallsAfterCancel == 0 {
			return true, "no tool call occurred after cancellation"
		}
		if !trace.Cancelled {
			return false, "trace was not cancelled"
		}
		return false, fmt.Sprintf("%d tool calls occurred after cancellation", trace.ToolCallsAfterCancel)
	case CheckVerifiedCompletion:
		status := strings.ToLower(strings.TrimSpace(trace.VerificationStatus))
		if trace.CompletionStatus == "" {
			return false, "completion status is missing"
		}
		if status == "passed" || status == "verified" {
			return true, "completion has an explicit verification status"
		}
		return false, fmt.Sprintf("verification status is %q", trace.VerificationStatus)
	case CheckExplicitEmptyOutput:
		for _, result := range trace.ToolResults {
			if strings.Contains(result.Text, "stdout_bytes=0") || strings.Contains(result.Text, `"stdout_bytes":0`) {
				return true, "empty stdout is explicit"
			}
		}
		return false, "tool result does not explicitly represent empty stdout"
	default:
		return false, fmt.Sprintf("unsupported check kind %q", criterion.Kind)
	}
}

// DefaultScenarios returns the initial deterministic quality baseline. The
// returned slice is sorted by stable ID so reports and future snapshots are
// reproducible.
func DefaultScenarios() []Scenario {
	scenarios := []Scenario{
		{
			ID: "qa-grounded", Name: "事实型回答", Category: "instruction-following",
			UserInput: "根据已提供的信息回答问题，不要编造未观察到的事实。",
			Criteria:  []Criterion{{ID: "answer-grounded", Kind: CheckFinalNotContains, Value: "我已经验证", Description: "没有验证证据时不应声称已经验证"}},
		},
		{
			ID: "workspace-read-verify", Name: "读取文件并说明结果", Category: "workspace",
			UserInput: "读取 data/report.txt，并告诉我文件是否存在以及内容摘要。",
			Criteria: []Criterion{
				{ID: "read-file", Kind: CheckToolCalled, Value: "read_file", Description: "使用文件读取工具"},
				{ID: "read-result", Kind: CheckResultContains, Value: "内容", Description: "最终依据工具返回内容"},
			},
		},
		{
			ID: "bash-empty-output", Name: "本地命令空输出", Category: "tool-result",
			UserInput: "执行一个成功但没有 stdout 的本地命令，并准确说明结果。",
			Criteria: []Criterion{
				{ID: "bash-call", Kind: CheckToolCalled, Value: "bash_run", Description: "使用本地命令工具"},
				{ID: "empty-output", Kind: CheckExplicitEmptyOutput, Description: "显式区分成功空输出和未执行"},
				{ID: "exit-zero", Kind: CheckResultContains, Value: "exit=0", Description: "依据退出码判断成功"},
			},
		},
		{
			ID: "linux-exec-sequence", Name: "远程 Linux 命令", Category: "remote-execution",
			UserInput: "在已绑定的 Linux 通道执行命令，并报告退出码和 stdout/stderr。",
			Criteria: []Criterion{
				{ID: "list-config", Kind: CheckToolCalled, Value: "terminal_config_list", Description: "先获取 Agent 可用配置"},
				{ID: "linux-call", Kind: CheckToolCalled, Value: "linux_exec", Description: "使用 Linux 远程执行工具"},
				{ID: "config-before-exec", Kind: CheckToolCallOrder, Value: "terminal_config_list>linux_exec", Description: "禁止猜测目标配置"},
				{ID: "linux-result", Kind: CheckResultContains, Value: "[LINUX_RESULT]", Description: "使用结构化远程结果"},
			},
		},
		{
			ID: "tool-failure-recovery", Name: "工具失败恢复", Category: "recovery",
			UserInput: "执行命令；如果失败，诊断原因并采取合理的替代方案。",
			Criteria: []Criterion{
				{ID: "attempt-command", Kind: CheckToolCalled, Value: "bash_run", Description: "确实执行命令"},
				{ID: "report-failure", Kind: CheckFinalContains, Value: "失败", Description: "不能掩盖工具失败"},
			},
		},
		{
			ID: "readonly-batch", Name: "独立只读工具批次", Category: "tool-scheduling",
			UserInput: "同时收集工作区中的文件列表和匹配项，最后合并报告。",
			Criteria: []Criterion{
				{ID: "glob", Kind: CheckToolCalled, Value: "glob_files", Description: "获取文件列表"},
				{ID: "grep", Kind: CheckToolCalled, Value: "grep_file", Description: "获取匹配内容"},
			},
		},
		{
			ID: "mutation-verification", Name: "写入后验证", Category: "verification",
			UserInput: "修改指定文件，并确认修改已经落盘。",
			Criteria: []Criterion{
				{ID: "write", Kind: CheckToolCalled, Value: "write_file", Description: "执行写入"},
				{ID: "read-back", Kind: CheckToolCalled, Value: "read_file", Description: "读取并验证写入结果"},
				{ID: "ordered-verification", Kind: CheckToolCallOrder, Value: "write_file>read_file", Description: "验证必须发生在写入之后"},
			},
		},
		{
			ID: "async-callback", Name: "异步任务回调", Category: "async",
			UserInput: "启动一个较长任务，等待系统回调后报告最终结果。",
			Criteria: []Criterion{
				{ID: "async-fact", Kind: CheckResultContains, Value: "async_tool_result", Description: "使用异步回调事实"},
				{ID: "callback-report", Kind: CheckFinalContains, Value: "回调", Description: "说明结果来自回调或最终状态"},
			},
		},
		{
			ID: "cancel-fencing", Name: "取消栅栏", Category: "cancellation",
			UserInput: "启动任务后立即取消，取消后不得再执行新的工具。",
			Criteria:  []Criterion{{ID: "cancel-fence", Kind: CheckNoToolsAfterCancel, Description: "取消后不产生新的工具调用"}},
		},
		{
			ID: "hitl-resume", Name: "高风险操作确认", Category: "hitl",
			UserInput: "执行需要确认的操作；等待用户确认后再继续。",
			Criteria:  []Criterion{{ID: "confirmation-result", Kind: CheckFinalContains, Value: "确认", Description: "说明确认边界和结果"}},
		},
		{
			ID: "skill-contract", Name: "Skill 执行契约", Category: "skills",
			UserInput: "加载与当前任务匹配的 skill，并按照其中的完成标准执行。",
			Criteria: []Criterion{
				{ID: "load-skill", Kind: CheckToolCalled, Value: "load_skills", Description: "按目录元数据加载匹配 skill"},
				{ID: "skill-evidence", Kind: CheckResultContains, Value: "验证", Description: "遵循 skill 的验证要求"},
			},
		},
		{
			ID: "verified-finalization", Name: "最终完成判定", Category: "completion",
			UserInput: "完成任务后只在有明确证据时报告成功。",
			Criteria:  []Criterion{{ID: "verified", Kind: CheckVerifiedCompletion, Description: "完成状态必须绑定验证状态"}},
		},
	}
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].ID < scenarios[j].ID })
	return scenarios
}
