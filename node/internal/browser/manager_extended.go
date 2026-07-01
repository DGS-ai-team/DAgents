package browser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func (m *Manager) resolveFSPath(relOrAbs string) (string, error) {
	p := strings.TrimSpace(relOrAbs)
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	root, err := filepath.Abs(strings.TrimSpace(m.fsRoot))
	if err != nil {
		return "", err
	}
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(p)))
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) && abs != root {
		return "", fmt.Errorf("path escapes fs_root")
	}
	return abs, nil
}

func (m *Manager) Search(ctx context.Context, sessionKey, query, engine string) (ToolResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return ToolResult{OK: false, Error: "query is required"}, nil
	}
	if engine == "" {
		engine = "duckduckgo"
	}
	return m.call(ctx, Request{Op: "search", SessionKey: sessionKey, Query: query, Engine: engine})
}

func (m *Manager) GoBack(ctx context.Context, sessionKey string) (ToolResult, error) {
	return m.call(ctx, Request{Op: "go_back", SessionKey: sessionKey})
}

func (m *Manager) Scroll(ctx context.Context, sessionKey string, down *bool, pages float64, index int) (ToolResult, error) {
	if pages <= 0 {
		pages = 1
	}
	return m.call(ctx, Request{
		Op:         "scroll",
		SessionKey: sessionKey,
		Down:       down,
		Pages:      pages,
		Index:      index,
	})
}

func (m *Manager) FindText(ctx context.Context, sessionKey, text string) (ToolResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ToolResult{OK: false, Error: "text is required"}, nil
	}
	return m.call(ctx, Request{Op: "find_text", SessionKey: sessionKey, Text: text})
}

func (m *Manager) SwitchTab(ctx context.Context, sessionKey, tabID string) (ToolResult, error) {
	tabID = strings.TrimSpace(tabID)
	if len(tabID) != 4 {
		return ToolResult{OK: false, Error: "tab_id must be 4 characters (see browser_snapshot detail.tabs)"}, nil
	}
	return m.call(ctx, Request{Op: "switch_tab", SessionKey: sessionKey, TabID: tabID})
}

func (m *Manager) CloseTab(ctx context.Context, sessionKey, tabID string) (ToolResult, error) {
	tabID = strings.TrimSpace(tabID)
	if len(tabID) != 4 {
		return ToolResult{OK: false, Error: "tab_id must be 4 characters"}, nil
	}
	return m.call(ctx, Request{Op: "close_tab", SessionKey: sessionKey, TabID: tabID})
}

func (m *Manager) Extract(ctx context.Context, sessionKey, query string, extractLinks, extractImages bool, startFromChar int, alreadyCollected []string) (ToolResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return ToolResult{OK: false, Error: "query is required"}, nil
	}
	return m.call(ctx, Request{
		Op:               "extract",
		SessionKey:       sessionKey,
		Query:            query,
		ExtractLinks:     extractLinks,
		ExtractImages:    extractImages,
		StartFromChar:    startFromChar,
		AlreadyCollected: alreadyCollected,
	})
}

func (m *Manager) Evaluate(ctx context.Context, sessionKey, code string) (ToolResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return ToolResult{OK: false, Error: "code is required"}, nil
	}
	return m.call(ctx, Request{Op: "evaluate", SessionKey: sessionKey, Code: code})
}

func (m *Manager) FindElements(ctx context.Context, sessionKey, selector string, attributes []string, maxResults int, includeText *bool) (ToolResult, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return ToolResult{OK: false, Error: "selector is required"}, nil
	}
	if maxResults <= 0 {
		maxResults = 50
	}
	return m.call(ctx, Request{
		Op:          "find_elements",
		SessionKey:  sessionKey,
		Selector:    selector,
		Attributes:  attributes,
		MaxResults:  maxResults,
		IncludeText: includeText,
	})
}

func (m *Manager) SearchPage(ctx context.Context, sessionKey, pattern string, regex, caseSensitive bool, contextChars, maxResults int, cssScope string) (ToolResult, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ToolResult{OK: false, Error: "pattern is required"}, nil
	}
	if contextChars <= 0 {
		contextChars = 150
	}
	if maxResults <= 0 {
		maxResults = 25
	}
	return m.call(ctx, Request{
		Op:            "search_page",
		SessionKey:    sessionKey,
		Pattern:       pattern,
		Regex:         regex,
		CaseSensitive: caseSensitive,
		ContextChars:  contextChars,
		MaxResults:    maxResults,
		CSSScope:      strings.TrimSpace(cssScope),
	})
}

func (m *Manager) UploadFile(ctx context.Context, sessionKey string, index int, relPath string) (ToolResult, error) {
	if index <= 0 {
		return ToolResult{OK: false, Error: "index is required"}, nil
	}
	abs, err := m.resolveFSPath(relPath)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	return m.call(ctx, Request{
		Op:         "upload_file",
		SessionKey: sessionKey,
		Index:      index,
		Path:       abs,
	})
}

func (m *Manager) DropdownOptions(ctx context.Context, sessionKey string, index int) (ToolResult, error) {
	if index <= 0 {
		return ToolResult{OK: false, Error: "index is required"}, nil
	}
	return m.call(ctx, Request{Op: "dropdown_options", SessionKey: sessionKey, Index: index})
}

func (m *Manager) SelectDropdown(ctx context.Context, sessionKey string, index int, text string) (ToolResult, error) {
	if index <= 0 {
		return ToolResult{OK: false, Error: "index is required"}, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ToolResult{OK: false, Error: "text is required"}, nil
	}
	return m.call(ctx, Request{
		Op:         "select_dropdown",
		SessionKey: sessionKey,
		Index:      index,
		Text:       text,
	})
}
