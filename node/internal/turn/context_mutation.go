package turn

import "strings"

// ContextMutation is one reason a model-visible context segment must be
// rebuilt. Keeping pending causes as a typed slice avoids making the internal
// state machine depend on a comma-delimited wire string.
type ContextMutation struct {
	Reason string `json:"reason"`
}

func appendContextMutation(previous []ContextMutation, reason string) []ContextMutation {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return previous
	}
	for _, mutation := range previous {
		if mutation.Reason == reason {
			return previous
		}
	}
	return append(previous, ContextMutation{Reason: reason})
}

func contextMutationReasons(mutations []ContextMutation) string {
	if len(mutations) == 0 {
		return ""
	}
	reasons := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		if reason := strings.TrimSpace(mutation.Reason); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	return strings.Join(reasons, ",")
}
