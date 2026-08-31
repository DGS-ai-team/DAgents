package tools

import (
	"encoding/json"
	"testing"
)

// TestToolDefinitionsRequiredAfterInject 校验 inject 后 parameters.required 与业务必填一致。
func TestToolDefinitionsRequiredAfterInject(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string][]string{
		"read_file":              {CallPurposeKey, "path"},
		"show_image":             {CallPurposeKey, "path"},
		"read_image":             {CallPurposeKey, "path"},
		"write_file":             {CallPurposeKey, "path", "content"},
		"search_replace":         {CallPurposeKey, "path", "old_string", "new_string"},
		"glob_files":             {CallPurposeKey, "directory", "glob_pattern"},
		"grep_file":              {CallPurposeKey, "path", "pattern"},
		"grep_files":             {CallPurposeKey, "directory", "pattern"},
		"bash_run":               {CallPurposeKey, "command"},
		"terminal_config_list":   {CallPurposeKey},
		"terminal_open":          {CallPurposeKey, "config_id"},
		"terminal_input":         {CallPurposeKey, "terminal_id", "data"},
		"terminal_read":          {CallPurposeKey, "terminal_id"},
		"terminal_terminate":     {CallPurposeKey, "terminal_id"},
		"terminal_list":          {CallPurposeKey},
		"terminal_command":       {CallPurposeKey, "terminal_id", "command"},
		"screen_capture":         {CallPurposeKey},
		"computer_use":           {CallPurposeKey, "action"},
		"background_job_status":  {CallPurposeKey, "job_id"},
		"background_job_cancel":  {CallPurposeKey, "job_id"},
		"ask_user_information":   {CallPurposeKey, "question"},
		"remember":               {CallPurposeKey, "information"},
		"load_skills":            {CallPurposeKey, "skill_names"},
		"unload_skills":          {CallPurposeKey, "skill_names"},
		"clear_skills":           {CallPurposeKey},
		"trigger_list":           {CallPurposeKey},
		"trigger_get":            {CallPurposeKey, "trigger_id"},
		"trigger_create":         {CallPurposeKey, "name", "task_template", "condition"},
		"trigger_update":         {CallPurposeKey, "trigger_id"},
		"trigger_delete":         {CallPurposeKey, "trigger_id"},
		"create_temporary_agent": {CallPurposeKey, "task", "purpose"},
		"wait_temporary_agents":  {CallPurposeKey, "child_agent_ids"},
		"temporary_agent_status": {CallPurposeKey, "child_agent_ids"},
		"cancel_temporary_agent": {CallPurposeKey, "child_agent_id"},
	}

	for _, def := range reg.Definitions() {
		name := def.Function.Name
		params := def.Function.Parameters
		req, ok := params["required"].([]string)
		if !ok {
			t.Fatalf("tool %q: required is not []string", name)
		}

		expected, listed := want[name]
		if !listed {
			t.Fatalf("tool %q: add to want map in test (got required=%v)", name, req)
		}
		if len(req) != len(expected) {
			t.Fatalf("tool %q: required len=%d want=%v got=%v", name, len(expected), expected, req)
		}
		for i := range expected {
			if req[i] != expected[i] {
				t.Fatalf("tool %q: required[%d]=%q want %q (full got=%v)", name, i, req[i], expected[i], req)
			}
		}

		// call_purpose 必须排在首位（inject 行为）。
		if len(req) > 0 && req[0] != CallPurposeKey {
			t.Fatalf("tool %q: call_purpose should be first, got %v", name, req)
		}

		// run_in_background 已移出 schema，不得出现在 properties。
		props, _ := params["properties"].(map[string]any)
		if _, ok := props[RunInBackgroundKey]; ok {
			t.Fatalf("tool %q: run_in_background must not appear in schema properties", name)
		}

		// required 中每项须在 properties 存在（顶层 object）。
		for _, field := range req {
			if _, ok := props[field]; !ok {
				t.Fatalf("tool %q: required field %q missing in properties", name, field)
			}
		}
	}
}

func TestParseToolCallArgumentsSearchReplaceShape(t *testing.T) {
	raw := `{"call_purpose":"替换表头","path":"data/x.txt","old_string":"a","new_string":"b","run_in_background":false}`
	bg, cleaned := ParseToolCallArguments(raw)
	if bg {
		t.Fatal("expected sync")
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(cleaned), &got); err != nil {
		t.Fatalf("cleaned json: %v", err)
	}
	want := map[string]string{
		"path":       "data/x.txt",
		"old_string": "a",
		"new_string": "b",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("field %q: got %q want %q (full=%v)", k, got[k], v, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected keys in cleaned: %v", got)
	}
}
