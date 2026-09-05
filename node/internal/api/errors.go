package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeAPIError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, errorBody{
		Error: errorDetail{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

// decodeJSON 解析入站 JSON；请求模型只消费当前 API 需要的字段。
func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func parseLastEventID(header string) int {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	n, err := strconv.Atoi(header)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
