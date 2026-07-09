package shared

import (
	"fmt"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

// HydrateTranscriptOptions 控制 hydrate 灌入 transcript 的行为。
type HydrateTranscriptOptions struct {
	ShowReasoning bool
	Verbose       bool
	ToolRegistry  *ToolBlockRegistry
}

// LoadTranscriptFromHydrate 将 hydrate transcript 写入 Transcript（F-H5）。
func LoadTranscriptFromHydrate(tr *Transcript, entries []nodeapi.TranscriptEntry, opts HydrateTranscriptOptions) {
	if tr == nil {
		return
	}
	tr.Reset()
	for _, raw := range entries {
		if raw == nil {
			continue
		}
		kind := strings.TrimSpace(fmt.Sprint(raw["kind"]))
		switch kind {
		case "user":
			text := strings.TrimSpace(fmt.Sprint(raw["text"]))
			if text != "" && text != "<nil>" {
				tr.Add("[user] " + text)
			}
			for _, hint := range UserMediaHintLines(raw) {
				tr.Add("    " + hint)
			}
		case "assistant":
			tr.FinishPartial("reasoning")
			text := strings.TrimSpace(fmt.Sprint(raw["text"]))
			if text != "" && text != "<nil>" {
				tr.Add("[assistant] " + text)
			}
		case "reasoning":
			if !opts.ShowReasoning {
				continue
			}
			text := strings.TrimSpace(fmt.Sprint(raw["text"]))
			if text != "" && text != "<nil>" {
				tr.Add("[reasoning] " + text)
			}
		case "tool_call", "tool_result":
			tr.FinishPartial("assistant")
			tr.FinishPartial("reasoning")
			data := mapFromAny(raw["data"])
			blockID := hydrateBlockID(raw, data)
			if opts.ToolRegistry != nil && blockID != "" {
				opts.ToolRegistry.Register(blockID)
			}
			for _, line := range FormatToolEventWithID(kind, data, blockID, opts.Verbose) {
				tr.Add(line)
			}
		}
	}
}

func hydrateBlockID(raw nodeapi.TranscriptEntry, data map[string]any) string {
	if id := strings.TrimSpace(fmt.Sprint(raw["blockId"])); id != "" && id != "<nil>" {
		return id
	}
	if data != nil {
		if id := ToolCallIDFromEvent(data); id != "" {
			return id
		}
		return ToolEventID(data)
	}
	return ""
}

func mapFromAny(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
