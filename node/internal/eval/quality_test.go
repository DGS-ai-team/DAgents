package eval

import "testing"

func TestDefaultScenariosAreStableAndCoverBaseline(t *testing.T) {
	scenarios := DefaultScenarios()
	if len(scenarios) != 12 {
		t.Fatalf("scenario count = %d, want 12", len(scenarios))
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
	for _, want := range []string{"bash-empty-output", "linux-exec-sequence", "async-callback", "cancel-fencing", "mutation-verification"} {
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
			Steps:              3,
			Retries:            2,
			DurationMS:         150,
			InputTokens:        100,
			OutputTokens:       25,
			Cost:               0.01,
			CacheObserved:      true,
			CacheHit:           true,
			ToolCalls:          []ToolCall{{Name: "bash_run"}},
			ToolResults:        []ToolResult{{Status: "failed", ExitCode: &exit}},
			VerificationStatus: "passed",
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
