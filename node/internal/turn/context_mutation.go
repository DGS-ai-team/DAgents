package turn

import "strings"

// appendContextMutation records a distinct reason for rebuilding the next
// model context snapshot. Reasons are internal strings because lifecycle
// events already expose one compact diagnostic reason.
func appendContextMutation(previous []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return previous
	}
	for _, mutation := range previous {
		if mutation == reason {
			return previous
		}
	}
	return append(previous, reason)
}

func contextMutationReasons(mutations []string) string {
	if len(mutations) == 0 {
		return ""
	}
	reasons := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		if reason := strings.TrimSpace(mutation); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	return strings.Join(reasons, ",")
}
