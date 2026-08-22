package eval

import "fmt"

// MinimumABScenarios is the minimum number of shared scenarios required before
// CompareAB emits a directional recommendation. A smaller sample still
// returns all deltas, but remains explicitly inconclusive.
const MinimumABScenarios = 3

// ABDecision is deliberately conservative: quality evidence is evaluated
// before cost, and missing cache observations never become a positive or
// negative cache conclusion.
type ABDecision string

const (
	ABDecisionTreatment    ABDecision = "treatment_supported"
	ABDecisionControl      ABDecision = "control_supported"
	ABDecisionInconclusive ABDecision = "inconclusive"
)

// ABComparison is a provider-neutral comparison of two runs over the same
// scenario catalogue. It is suitable for reports and does not decide whether
// an observed cost increase is acceptable outside the quality gates below.
type ABComparison struct {
	Treatment Scorecard `json:"treatment"`
	Control   Scorecard `json:"control"`

	ScenarioCount   int `json:"scenario_count"`
	ScenarioOverlap int `json:"scenario_overlap"`

	TaskSuccessRateDelta        float64 `json:"task_success_rate_delta"`
	CriterionPassRateDelta      float64 `json:"criterion_pass_rate_delta"`
	VerificationCoverageDelta   float64 `json:"verification_coverage_delta"`
	CancellationViolationsDelta int     `json:"cancellation_violations_delta"`
	TotalToolFailuresDelta      int     `json:"total_tool_failures_delta"`
	InputTokensDelta            int     `json:"input_tokens_delta"`
	OutputTokensDelta           int     `json:"output_tokens_delta"`
	TotalStepsDelta             int     `json:"total_steps_delta"`
	TotalToolCallsDelta         int     `json:"total_tool_calls_delta"`
	TotalRetriesDelta           int     `json:"total_retries_delta"`
	TotalCostDelta              float64 `json:"total_cost_delta"`

	CacheComparable           bool    `json:"cache_comparable"`
	TreatmentCacheObservation float64 `json:"treatment_cache_observation_rate"`
	ControlCacheObservation   float64 `json:"control_cache_observation_rate"`
	CacheHitRateDelta         float64 `json:"cache_hit_rate_delta"`

	Decision ABDecision `json:"decision"`
	Reason   string     `json:"reason"`
}

// CompareAB evaluates treatment and control against the same scenarios. The
// returned scorecards retain every scenario result, including missing traces.
// This makes incomplete experiments visible instead of silently dropping
// failed or unavailable samples.
func CompareAB(scenarios []Scenario, treatment, control map[string]Trace) ABComparison {
	treatmentScore := EvaluateSuite(scenarios, treatment)
	controlScore := EvaluateSuite(scenarios, control)
	comparison := ABComparison{
		Treatment:                   treatmentScore,
		Control:                     controlScore,
		ScenarioCount:               len(scenarios),
		ScenarioOverlap:             traceOverlap(scenarios, treatment, control),
		TaskSuccessRateDelta:        treatmentScore.TaskSuccessRate - controlScore.TaskSuccessRate,
		CriterionPassRateDelta:      treatmentScore.CriterionPassRate - controlScore.CriterionPassRate,
		VerificationCoverageDelta:   treatmentScore.VerificationCoverage - controlScore.VerificationCoverage,
		CancellationViolationsDelta: treatmentScore.CancellationViolations - controlScore.CancellationViolations,
		TotalToolFailuresDelta:      treatmentScore.TotalToolFailures - controlScore.TotalToolFailures,
		InputTokensDelta:            treatmentScore.TotalInputTokens - controlScore.TotalInputTokens,
		OutputTokensDelta:           treatmentScore.TotalOutputTokens - controlScore.TotalOutputTokens,
		TotalStepsDelta:             treatmentScore.TotalSteps - controlScore.TotalSteps,
		TotalToolCallsDelta:         treatmentScore.TotalToolCalls - controlScore.TotalToolCalls,
		TotalRetriesDelta:           treatmentScore.TotalRetries - controlScore.TotalRetries,
		TotalCostDelta:              treatmentScore.TotalCost - controlScore.TotalCost,
		TreatmentCacheObservation:   observationRate(treatmentScore),
		ControlCacheObservation:     observationRate(controlScore),
		Decision:                    ABDecisionInconclusive,
	}
	comparison.CacheComparable = cacheComparable(treatmentScore, controlScore)
	if comparison.CacheComparable {
		comparison.CacheHitRateDelta = treatmentScore.PromptCacheHitRate - controlScore.PromptCacheHitRate
	}
	comparison.Decision, comparison.Reason = decideAB(comparison)
	return comparison
}

func traceOverlap(scenarios []Scenario, treatment, control map[string]Trace) int {
	if len(scenarios) == 0 {
		return 0
	}
	count := 0
	for _, scenario := range scenarios {
		if _, treatmentOK := treatment[scenario.ID]; !treatmentOK {
			continue
		}
		if _, controlOK := control[scenario.ID]; controlOK {
			count++
		}
	}
	return count
}

func observationRate(score Scorecard) float64 {
	if score.ScenarioCount <= 0 {
		return 0
	}
	return float64(score.CacheObservedCount) / float64(score.ScenarioCount)
}

func cacheComparable(treatment, control Scorecard) bool {
	return treatment.ScenarioCount > 0 &&
		treatment.CacheObservedCount == treatment.ScenarioCount &&
		control.CacheObservedCount == control.ScenarioCount
}

func decideAB(comparison ABComparison) (ABDecision, string) {
	if comparison.ScenarioCount < MinimumABScenarios || comparison.ScenarioOverlap < comparison.ScenarioCount {
		return ABDecisionInconclusive, fmt.Sprintf(
			"需要至少 %d 个双方都有结果的场景，当前为 %d/%d",
			MinimumABScenarios, comparison.ScenarioOverlap, comparison.ScenarioCount,
		)
	}

	qualityNoWorse := comparison.TaskSuccessRateDelta >= 0 &&
		comparison.CriterionPassRateDelta >= 0 &&
		comparison.VerificationCoverageDelta >= 0 &&
		comparison.CancellationViolationsDelta <= 0 &&
		comparison.TotalToolFailuresDelta <= 0
	qualityImproved := comparison.TaskSuccessRateDelta > 0 ||
		comparison.CriterionPassRateDelta > 0 ||
		comparison.VerificationCoverageDelta > 0 ||
		comparison.CancellationViolationsDelta < 0 ||
		comparison.TotalToolFailuresDelta < 0
	costLower := comparison.InputTokensDelta < 0 || comparison.TotalCostDelta < 0

	if qualityNoWorse && (qualityImproved || costLower) {
		if qualityImproved {
			return ABDecisionTreatment, "treatment 的质量指标提升且没有观察到质量回退；成本变化单独记录，不覆盖质量结论"
		}
		return ABDecisionTreatment, "质量指标不下降且 treatment 的输入 token 或成本更低"
	}
	if !qualityNoWorse {
		return ABDecisionControl, "treatment 至少一个质量或安全指标回退"
	}
	return ABDecisionInconclusive, "质量与成本没有形成足够的方向性证据"
}
