package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
)

// createGoalViaAutonomy keeps legacy integration assertions focused on the
// returned Goal while exercising the public Auto Agent endpoint.
func createGoalViaAutonomy(srv *Server, agentID string, body []byte) *httptest.ResponseRecorder {
	var input map[string]any
	if err := json.Unmarshal(body, &input); err != nil {
		return httptest.NewRecorder()
	}
	delete(input, "agent_id")
	encoded, err := json.Marshal(input)
	if err != nil {
		return httptest.NewRecorder()
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/v1/agents/"+url.PathEscape(agentID)+"/autonomy", bytes.NewReader(encoded)))
	if w.Code >= 200 && w.Code < 300 {
		var envelope struct {
			Limits json.RawMessage `json:"limits"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err == nil && len(envelope.Limits) > 0 {
			w.Body.Reset()
			_, _ = w.Body.Write(envelope.Limits)
		}
	}
	return w
}
