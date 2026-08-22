package eval

import (
	"strings"
	"testing"
)

func TestDefaultScenariosAreStableAndCoverBaseline(t *testing.T) {
	scenarios := DefaultScenarios()
	if len(scenarios) != 17 {
		t.Fatalf("scenario count = %d, want 17", len(scenarios))
	}
	seen := make(map[string]struct{}, len(scenarios))
	previous := ""
	for _, scenario := range scenarios {
		if scenario.ID == "" || scenario.Name == "" || scenario.UserInput == "" || len(scenario.Criteria) == 0 {
			t.Fatalf("incomplete scenario: %+v", scenario)
		}
		if _, ok := seen[scenario.ID]; ok {
			t.Fatalf("duplicate scenario id %q", scenario.ID)
		}
		seen[scenario.ID] = struct{}{}
		if previous != "" && previous >= scenario.ID {
			t.Fatalf("scenarios are not sorted: %q before %q", previous, scenario.ID)
		}
		previous = scenario.ID
	}
	for _, want := range []string{"bash-empty-output", "linux-exec-sequence", "async-callback", "cancel-fencing", "mutation-verification", "skill-catalog-boundary", "skill-load-ambiguous", "skill-load-boundary", "skill-load-diagnostics", "skill-unload-boundary"} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("missing baseline scenario %q", want)
		}
	}
}

func TestEvaluateScenarioChecksToolOrderAndEmptyOutput(t *testing.T) {
	exit := 0
	scenario := Scenario{
		ID: "test",
		Criteria: []Criterion{
			{ID: "order", Kind: CheckToolCallOrder, Value: "terminal_config_list>linux_exec"},
			{ID: "empty", Kind: CheckExplicitEmptyOutput},
		},
	}
	result := EvaluateScenario(scenario, Trace{
		ScenarioID:  "test",
		ToolCalls:   []ToolCall{{Name: "terminal_config_list"}, {Name: "linux_exec"}},
		ToolResults: []ToolResult{{Name: "linux_exec", Text: "[LINUX_RESULT] exit=0\nstdout_bytes=0", ExitCode: &exit}},
	})
	if !result.Passed {
		t.Fatalf("result=%+v", result)
	}
}

func TestEvaluateSuiteTreatsMissingTraceAsFailure(t *testing.T) {
	scenarios := []Scenario{{ID: "missing", Criteria: []Criterion{{Kind: CheckToolCalled, Value: "bash_run"}}}}
	score := EvaluateSuite(scenarios, nil)
	if score.ScenarioPassCount != 0 || score.TaskSuccessRate != 0 {
		t.Fatalf("score=%+v", score)
	}
	if len(score.Results) != 1 || score.Results[0].Passed {
		t.Fatalf("results=%+v", score.Results)
	}
}

func TestEvaluateSuiteAggregatesRuntimeAndCacheMetrics(t *testing.T) {
	exit := 1
	scenarios := []Scenario{{ID: "metrics", Criteria: nil}}
	score := EvaluateSuite(scenarios, map[string]Trace{
		"metrics": {
			Steps:                 3,
			Retries:               2,
			DurationMS:            150,
			InputTokens:           100,
			OutputTokens:          25,
			Cost:                  0.01,
			CacheObserved:         true,
			CacheHit:              true,
			PromptCacheHitTokens:  80,
			PromptCacheMissTokens: 20,
			ToolCalls:             []ToolCall{{Name: "bash_run"}},
			ToolResults:           []ToolResult{{Status: "failed", ExitCode: &exit}},
			VerificationStatus:    "passed",
		},
	})
	if score.TotalSteps != 3 || score.TotalRetries != 2 || score.TotalDurationMS != 150 || score.TotalInputTokens != 100 || score.TotalOutputTokens != 25 {
		t.Fatalf("runtime metrics=%+v", score)
	}
	if score.TotalToolCalls != 1 || score.TotalToolFailures != 1 || score.TotalCost != 0.01 {
		t.Fatalf("tool metrics=%+v", score)
	}
	if score.CacheObservedCount != 1 || score.CacheHitCount != 1 || score.VerificationCoverage != 1 {
		t.Fatalf("cache/verification metrics=%+v", score)
	}
	if score.TotalPromptCacheHitTokens != 80 || score.TotalPromptCacheMissTokens != 20 || score.PromptCacheHitRate != 0.8 {
		t.Fatalf("cache token metrics=%+v", score)
	}
}

func TestEvaluateSuiteIgnoresUnobservedCacheTokenClaims(t *testing.T) {
	score := EvaluateSuite([]Scenario{{ID: "unknown", Criteria: nil}}, map[string]Trace{
		"unknown": {
			PromptCacheHitTokens:  100,
			PromptCacheMissTokens: 0,
			CacheObserved:         false,
		},
	})
	if score.CacheObservedCount != 0 || score.TotalPromptCacheHitTokens != 0 || score.PromptCacheHitRate != 0 {
		t.Fatalf("unobserved cache was aggregated = %+v", score)
	}
}

func TestEvaluateScenarioRequiresExplicitVerification(t *testing.T) {
	scenario := Scenario{ID: "verified", Criteria: []Criterion{{Kind: CheckVerifiedCompletion}}}
	for _, status := range []string{"", "unverified", "partial", "failed"} {
		result := EvaluateScenario(scenario, Trace{CompletionStatus: "complete", VerificationStatus: status})
		if result.Passed {
			t.Fatalf("status %q unexpectedly passed", status)
		}
	}
	result := EvaluateScenario(scenario, Trace{CompletionStatus: "complete", VerificationStatus: "passed"})
	if !result.Passed {
		t.Fatalf("verified result=%+v", result)
	}
}

func TestSkillQualityScenariosPassStructuredLoadTrace(t *testing.T) {
	traces := map[string]Trace{
		"skill-contract": {
			ToolCalls:   []ToolCall{{Name: "load_skills"}},
			ToolResults: []ToolResult{{Name: "load_skills", Text: `{"action":"set_loaded_skills","loaded_skills":[{"skill_name":"writer"}],"model_context_applied_boundary":"next_model_step","verification":"验证"}`}},
			FinalText:   "已按 skill 执行并完成验证。",
		},
		"skill-load-boundary": {
			ToolCalls:   []ToolCall{{Name: "load_skills"}},
			ToolResults: []ToolResult{{Name: "load_skills", Text: `{"requested":["writer"],"loaded_skills":[{"skill_name":"writer"}],"session_state_applied_boundary":"immediate","model_context_applied_boundary":"next_model_step"}`}},
		},
		"skill-load-diagnostics": {
			ToolCalls:   []ToolCall{{Name: "load_skills"}},
			ToolResults: []ToolResult{{Name: "load_skills", Text: `{"requested":["writer","missing"],"loaded_skills":[{"skill_name":"writer"}],"rejected":[{"name":"missing","reason":"not_found"}]}`}},
		},
		"skill-load-ambiguous": {
			ToolCalls:   []ToolCall{{Name: "load_skills"}},
			ToolResults: []ToolResult{{Name: "load_skills", Text: `{"requested":["writer"],"loaded_skills":[],"rejected":[{"name":"writer","reason":"ambiguous"}]}`}},
		},
		"skill-catalog-boundary": {
			ToolCalls:   []ToolCall{{Name: "load_skills"}},
			ToolResults: []ToolResult{{Name: "load_skills", Text: `{"requested":["writer"],"loaded_skills":[],"rejected":[{"name":"writer","reason":"catalog_changed"}]}`}},
			FinalText:   "新版本将在下一次 human Turn 生效。",
		},
		"skill-unload-boundary": {
			ToolCalls:   []ToolCall{{Name: "unload_skills"}},
			ToolResults: []ToolResult{{Name: "unload_skills", Text: `{"loaded_skills":[],"model_context_applied_boundary":"next_model_step"}`}},
		},
	}
	var scenarios []Scenario
	for _, scenario := range DefaultScenarios() {
		if strings.HasPrefix(scenario.ID, "skill-") {
			scenarios = append(scenarios, scenario)
		}
	}
	score := EvaluateSuite(scenarios, traces)
	if score.ScenarioPassCount != len(scenarios) || score.CriterionPassRate != 1 {
		t.Fatalf("skill score=%+v", score)
	}
}
