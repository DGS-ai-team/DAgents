package eval

import "testing"

func abScenarios() []Scenario {
	return []Scenario{
		{ID: "a", Criteria: []Criterion{{Kind: CheckFinalContains, Value: "ok"}}},
		{ID: "b", Criteria: []Criterion{{Kind: CheckFinalContains, Value: "ok"}}},
		{ID: "c", Criteria: []Criterion{{Kind: CheckFinalContains, Value: "ok"}}},
	}
}

func passingABTraces(inputTokens int) map[string]Trace {
	return map[string]Trace{
		"a": {FinalText: "ok", InputTokens: inputTokens, CacheObserved: true, PromptCacheHitTokens: 80, PromptCacheMissTokens: 20},
		"b": {FinalText: "ok", InputTokens: inputTokens, CacheObserved: true, PromptCacheHitTokens: 80, PromptCacheMissTokens: 20},
		"c": {FinalText: "ok", InputTokens: inputTokens, CacheObserved: true, PromptCacheHitTokens: 80, PromptCacheMissTokens: 20},
	}
}

func TestCompareABSupportsQualityImprovementAndKeepsCostSeparate(t *testing.T) {
	control := passingABTraces(900)
	control["a"] = Trace{FinalText: "no", InputTokens: 900, CacheObserved: true, PromptCacheHitTokens: 80, PromptCacheMissTokens: 20}
	control["b"] = Trace{FinalText: "no", InputTokens: 900, CacheObserved: true, PromptCacheHitTokens: 80, PromptCacheMissTokens: 20}
	comparison := CompareAB(abScenarios(), passingABTraces(1200), control)
	if comparison.Decision != ABDecisionTreatment {
		t.Fatalf("decision = %s, reason=%s", comparison.Decision, comparison.Reason)
	}
	if comparison.TaskSuccessRateDelta <= 0 || comparison.InputTokensDelta <= 0 {
		t.Fatalf("deltas = %+v", comparison)
	}
	if !comparison.CacheComparable || comparison.CacheHitRateDelta != 0 {
		t.Fatalf("cache comparison = %+v", comparison)
	}
}

func TestCompareABSupportsLowerCostWhenQualityIsEqual(t *testing.T) {
	treatment := passingABTraces(800)
	control := passingABTraces(1200)
	comparison := CompareAB(abScenarios(), treatment, control)
	if comparison.Decision != ABDecisionTreatment {
		t.Fatalf("decision = %s, reason=%s", comparison.Decision, comparison.Reason)
	}
	if comparison.TaskSuccessRateDelta != 0 || comparison.InputTokensDelta >= 0 {
		t.Fatalf("deltas = %+v", comparison)
	}
}

func TestCompareABRejectsTreatmentQualityRegression(t *testing.T) {
	treatment := map[string]Trace{
		"a": {FinalText: "no"},
		"b": {FinalText: "no"},
		"c": {FinalText: "ok"},
	}
	comparison := CompareAB(abScenarios(), treatment, passingABTraces(1000))
	if comparison.Decision != ABDecisionControl {
		t.Fatalf("decision = %s, reason=%s", comparison.Decision, comparison.Reason)
	}
}

func TestCompareABDoesNotClaimCacheWhenProviderOmittedMetrics(t *testing.T) {
	comparison := CompareAB(abScenarios(), passingABTraces(1000), map[string]Trace{
		"a": {FinalText: "ok", PromptCacheHitTokens: 80},
		"b": {FinalText: "ok", PromptCacheHitTokens: 80},
		"c": {FinalText: "ok", PromptCacheHitTokens: 80},
	})
	if comparison.CacheComparable || comparison.ControlCacheObservation != 0 || comparison.CacheHitRateDelta != 0 {
		t.Fatalf("unobserved cache was compared = %+v", comparison)
	}
}

func TestCompareABRequiresSharedMinimumSample(t *testing.T) {
	comparison := CompareAB(abScenarios()[:2], passingABTraces(800), passingABTraces(1200))
	if comparison.Decision != ABDecisionInconclusive {
		t.Fatalf("decision = %s, reason=%s", comparison.Decision, comparison.Reason)
	}
}
