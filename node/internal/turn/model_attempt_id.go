package turn

import (
	"strconv"
	"strings"
)

// ModelAttemptID constructs the shared stable identity for one provider
// request attempt. Request/body digests are deliberately not identity inputs.
func ModelAttemptID(turnID, stepID string, attempt int) string {
	turnID = strings.TrimSpace(turnID)
	stepID = strings.TrimSpace(stepID)
	if turnID == "" || stepID == "" || attempt <= 0 {
		return ""
	}
	return turnID + "/" + stepID + "/attempt/" + strconv.Itoa(attempt)
}
