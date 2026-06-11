package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func readFileBody(out string) string {
	parts := strings.SplitN(out, "---\n", 2)
	if len(parts) != 2 {
		return out
	}
	return parts[1]
}

func TestReadFilePagination(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{"line1", "line2", "line3", "line4", "line5"}, "\n")
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"p.txt","content":`+mustJSON(content)+`}`)
	if err != nil {
		t.Fatal(err)
	}

	out, err := reg.Execute(ctx, "read_file", `{"path":"p.txt","line_offset":2,"line_limit":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "本页行区间: 2-3 / 5") {
		t.Fatalf("header missing range: %q", out)
	}
	if !strings.Contains(out, "next_line_offset: 4") {
		t.Fatalf("missing next offset: %q", out)
	}
	body := readFileBody(out)
	if body != "line2\nline3" {
		t.Fatalf("body = %q", body)
	}

	out2, err := reg.Execute(ctx, "read_file", `{"path":"p.txt","line_offset":4,"line_limit":10,"include_line_numbers":true}`)
	if err != nil {
		t.Fatal(err)
	}
	body2 := readFileBody(out2)
	if !strings.Contains(body2, "4  line4") || !strings.Contains(body2, "5  line5") {
		t.Fatalf("numbered body = %q", body2)
	}
	if strings.Contains(body2, "\t") {
		t.Fatalf("numbered body should not contain tab: %q", body2)
	}
	if !strings.Contains(out2, "next_line_offset: 无") {
		t.Fatalf("expected no more pages: %q", out2)
	}
}

func TestGrepFilePagination(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"alpha",
		"beta foo",
		"gamma",
		"delta foo bar",
		"epsilon foo",
	}, "\n")
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"s.txt","content":`+mustJSON(content)+`}`)
	if err != nil {
		t.Fatal(err)
	}

	out, err := reg.Execute(ctx, "grep_file", `{"path":"s.txt","pattern":"foo","literal":true,"count_limit":2,"context_lines":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "全文件命中数: 3") {
		t.Fatalf("hit count: %q", out)
	}
	if !strings.Contains(out, "next_index_offset: 2") {
		t.Fatalf("next index: %q", out)
	}
	if !strings.Contains(out, "beta foo") || !strings.Contains(out, "delta foo bar") {
		t.Fatalf("missing hit lines: %q", out)
	}
	if strings.Contains(out, "epsilon") {
		t.Fatalf("should not include third hit on first page: %q", out)
	}

	out2, err := reg.Execute(ctx, "grep_file", `{"path":"s.txt","pattern":"foo","literal":true,"index_offset":2,"count_limit":5}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "epsilon foo") {
		t.Fatalf("page2 missing hit: %q", out2)
	}
	if !strings.Contains(out2, "next_index_offset: 无") {
		t.Fatalf("page2 should be last: %q", out2)
	}
}

func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
