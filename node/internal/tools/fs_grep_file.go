package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

type grepFileArgs struct {
	Path          string  `json:"path"`
	Pattern       string  `json:"pattern"`
	IndexOffset   *int    `json:"index_offset"`
	CountLimit    *int    `json:"count_limit"`
	ContextLines  *int    `json:"context_lines"`
	CaseSensitive bool    `json:"case_sensitive"`
	Literal       bool    `json:"literal"`
	Encoding      *string `json:"encoding"`
}

func (r *Registry) execGrepFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args grepFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	return r.execGrepFileArgs(args, "index_offset")
}

func (r *Registry) execGrepFileArgs(args grepFileArgs, cursorName string) (string, error) {
	re, err := compileLinePattern(args.Pattern, args.Literal, args.CaseSensitive)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	opt := lineSearchOptions{
		rawPattern:    args.Pattern,
		literal:       args.Literal,
		caseSensitive: args.CaseSensitive,
		contextLines:  defaultSearchContextLines,
	}
	if args.IndexOffset != nil && *args.IndexOffset >= 0 {
		opt.indexOffset = *args.IndexOffset
	}
	if args.CountLimit != nil && *args.CountLimit >= 1 {
		opt.countLimit = *args.CountLimit
	}
	if args.ContextLines != nil {
		opt.contextLines = *args.ContextLines
	}
	opt.fileEncoding = args.Encoding
	opt.cursorName = cursorName
	return r.grepSingleFile(args.Path, re, opt)
}
