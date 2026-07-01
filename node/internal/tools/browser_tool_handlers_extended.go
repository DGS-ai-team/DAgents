package tools

import (
	"context"
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

func (r *Registry) registerBrowserExtendedTools() {
	r.handlers["browser_search"] = r.execBrowserSearch
	r.handlers["browser_go_back"] = r.execBrowserGoBack
	r.handlers["browser_scroll"] = r.execBrowserScroll
	r.handlers["browser_find_text"] = r.execBrowserFindText
	r.handlers["browser_switch_tab"] = r.execBrowserSwitchTab
	r.handlers["browser_close_tab"] = r.execBrowserCloseTab
	r.handlers["browser_extract"] = r.execBrowserExtract
	r.handlers["browser_evaluate"] = r.execBrowserEvaluate
	r.handlers["browser_find_elements"] = r.execBrowserFindElements
	r.handlers["browser_search_page"] = r.execBrowserSearchPage
	r.handlers["browser_upload_file"] = r.execBrowserUploadFile
	r.handlers["browser_dropdown_options"] = r.execBrowserDropdownOptions
	r.handlers["browser_select_dropdown"] = r.execBrowserSelectDropdown
}

func (r *Registry) execBrowserSearch(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Query  string `json:"query"`
		Engine string `json:"engine"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Search(ctx, sid, args.Query, args.Engine)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserGoBack(ctx context.Context, _ json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	out, err := r.browser.GoBack(ctx, sid)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserScroll(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Down  *bool   `json:"down"`
		Pages float64 `json:"pages"`
		Index int     `json:"index"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	out, err := r.browser.Scroll(ctx, sid, args.Down, args.Pages, args.Index)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserFindText(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.FindText(ctx, sid, args.Text)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserSwitchTab(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		TabID string `json:"tab_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.SwitchTab(ctx, sid, args.TabID)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserCloseTab(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		TabID string `json:"tab_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.CloseTab(ctx, sid, args.TabID)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserExtract(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Query            string   `json:"query"`
		ExtractLinks     bool     `json:"extract_links"`
		ExtractImages    bool     `json:"extract_images"`
		StartFromChar    int      `json:"start_from_char"`
		AlreadyCollected []string `json:"already_collected"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Extract(ctx, sid, args.Query, args.ExtractLinks, args.ExtractImages, args.StartFromChar, args.AlreadyCollected)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserEvaluate(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Evaluate(ctx, sid, args.Code)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserFindElements(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Selector    string   `json:"selector"`
		Attributes  []string `json:"attributes"`
		MaxResults  int      `json:"max_results"`
		IncludeText *bool    `json:"include_text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.FindElements(ctx, sid, args.Selector, args.Attributes, args.MaxResults, args.IncludeText)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserSearchPage(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Pattern       string `json:"pattern"`
		Regex         bool   `json:"regex"`
		CaseSensitive bool   `json:"case_sensitive"`
		ContextChars  int    `json:"context_chars"`
		CSSScope      string `json:"css_scope"`
		MaxResults    int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.SearchPage(ctx, sid, args.Pattern, args.Regex, args.CaseSensitive, args.ContextChars, args.MaxResults, args.CSSScope)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserUploadFile(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Index int    `json:"index"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.UploadFile(ctx, sid, args.Index, args.Path)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserDropdownOptions(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.DropdownOptions(ctx, sid, args.Index)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserSelectDropdown(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Index int    `json:"index"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.SelectDropdown(ctx, sid, args.Index, args.Text)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}
