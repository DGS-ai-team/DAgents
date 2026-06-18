package tools

import (
	"context"
	"strings"
	"testing"
)

func TestReadFile_default100LinesFits3000TokenBudget(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	// 典型代码行 ~40 ASCII 字符 ≈ 12 tokens；100 行 ≈ 1200 tokens < 3000
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "func line"+strings.Repeat("x", 32)+"() {}")
	}
	content := strings.Join(lines, "\n")
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"code.txt","content":`+mustJSON(content)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "read_file", `{"path":"code.txt","line_offset":1,"line_limit":100}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "本页内容是否因 token 上限截断: 是") {
		t.Fatalf("100 typical lines should not truncate at 3000 tokens: %q", out)
	}
}
