package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/toolresult"
)

func TestReadFile_truncatesByTokenBudget(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	// 每行 60 个「测」≈ 36 tokens；120 行 ≈ 4320 tokens > defaultReadMaxTokens(3000)
	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, strings.Repeat("测", 60))
	}
	content := strings.Join(lines, "\n")
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"big.txt","content":`+mustJSON(content)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "read_file", `{"path":"big.txt","line_offset":1,"line_limit":120}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "本页内容是否因 token 上限截断: 是") {
		t.Fatalf("expected token truncation flag: %q", out)
	}
	if !strings.Contains(out, "[TRUNCATED]") {
		t.Fatal("missing TRUNCATED marker")
	}
	body := readFileBody(out)
	if toolresult.EstimateTokens(body) > float64(defaultReadMaxTokens)+50 {
		t.Fatalf("body tokens=%v", toolresult.EstimateTokens(body))
	}
}

func TestGrepFile_truncatesByTokenBudget(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "hit "+strings.Repeat("x", 80))
	}
	content := strings.Join(lines, "\n")
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"g.txt","content":`+mustJSON(content)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "grep_file", `{"path":"g.txt","pattern":"hit","literal":true,"count_limit":200,"context_lines":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[TRUNCATED]") {
		t.Fatalf("expected grep truncation: len=%d", len(out))
	}
	if toolresult.EstimateTokens(out) > float64(defaultSearchMaxTokens)+120 {
		t.Fatalf("grep output tokens=%v", toolresult.EstimateTokens(out))
	}
}
