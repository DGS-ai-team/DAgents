package turn

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

// SkillInstructions are deliberately model-visible user-role context items,
// not part of the system prompt. The body is persisted in session history at
// activation time, matching the Codex model: the catalog tells the model what
// exists, while an activated skill contributes its full instructions only once.
const (
	skillInstructionsTag      = "<skill_instructions>"
	skillInstructionsCloseTag = "</skill_instructions>"
)

func buildSkillInstructionMessage(content skills.LoadedSkillContent) llm.Message {
	name := strings.TrimSpace(content.SkillName)
	directory := strings.TrimSpace(content.DirectoryName)
	if directory == "" {
		directory = name
	}
	body := strings.TrimSpace(content.Content)
	digest := Digest(struct {
		Name      string `json:"name"`
		Directory string `json:"directory"`
		Content   string `json:"content"`
	}{name, directory, body})
	text := fmt.Sprintf(
		"%s\n<name>%s</name>\n<path>skills/%s/SKILL.md</path>\n<content_digest>%s</content_digest>\n<instructions>\n%s\n</instructions>\n%s",
		skillInstructionsTag,
		name,
		directory,
		digest,
		body,
		skillInstructionsCloseTag,
	)
	source := llm.MessageSource{Kind: llm.MessageSourcePlugin, Form: llm.MessageFormInstructions}
	provenance := &llm.MessageProvenance{
		Producer:  "skills",
		Operation: "load",
		Reference: directory,
	}
	return llm.UserMessageWithSource(text, llm.UserNameSkill, source, provenance)
}

func (o *Orchestrator) activeSkillInstructionMessages() []llm.Message {
	if o == nil || o.skillAccess.Catalog == nil || !o.skillAccess.Catalog.Enabled() || o.skillAccess.Get == nil {
		return nil
	}
	loaded := o.skillAccess.Get()
	contents := o.skillAccess.Catalog.ReadLoadedSkillContents(loaded)
	if len(contents) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(contents))
	for _, content := range contents {
		out = append(out, buildSkillInstructionMessage(content))
	}
	return out
}

// ensureLoadedSkillInstructions persists missing skill body messages. A skill
// activated by load_skills is placed after the current tool results; a skill
// already selected by control-plane/session configuration is placed before the
// current root user message. Both placements are stable history positions and
// neither is a dynamic request tail.
func (o *Orchestrator) ensureLoadedSkillInstructions(sessionID string, history *[]llm.Message) {
	if o == nil || history == nil {
		return
	}
	active := o.activeSkillInstructionMessages()
	if len(active) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(*history))
	for _, message := range *history {
		if llm.IsMessageSource(message, llm.MessageSourcePlugin, llm.MessageFormInstructions, "") {
			existing[message.Content] = struct{}{}
		}
	}
	pending := make([]llm.Message, 0, len(active))
	for _, message := range active {
		if _, ok := existing[message.Content]; ok {
			continue
		}
		pending = append(pending, message)
	}
	if len(pending) == 0 {
		return
	}
	insertAt := skillInstructionInsertAt(*history)
	for i, message := range pending {
		o.insertHistory(sessionID, history, insertAt+i, message)
	}
}

func skillInstructionInsertAt(history []llm.Message) int {
	rootUser := -1
	latestSkillToolResult := -1
	for i, message := range history {
		if isContextRootUser(message) {
			rootUser = i
		}
		if message.Role == "tool" && message.Name == "load_skills" {
			latestSkillToolResult = i
		}
	}
	if latestSkillToolResult >= rootUser && latestSkillToolResult >= 0 {
		return len(history)
	}
	if rootUser >= 0 {
		return rootUser
	}
	return len(history)
}

// filterSkillInstructionMessages keeps only the latest active body for each
// skill in the outbound request. Durable history is never rewritten. This is
// what preserves unload semantics and prevents an old on-disk body from
// competing with a newly activated version after a skill edit.
func (o *Orchestrator) filterSkillInstructionMessages(history []llm.Message) []llm.Message {
	active := o.activeSkillInstructionMessages()
	activeContent := make(map[string]struct{}, len(active))
	for _, message := range active {
		activeContent[message.Content] = struct{}{}
	}
	lastActive := make(map[string]int, len(active))
	for i, message := range history {
		if !llm.IsMessageSource(message, llm.MessageSourcePlugin, llm.MessageFormInstructions, "") {
			continue
		}
		if _, ok := activeContent[message.Content]; ok {
			lastActive[message.Content] = i
		}
	}
	if len(lastActive) == 0 {
		out := make([]llm.Message, 0, len(history))
		for _, message := range history {
			if llm.IsMessageSource(message, llm.MessageSourcePlugin, llm.MessageFormInstructions, "") {
				continue
			}
			out = append(out, message)
		}
		return out
	}
	out := make([]llm.Message, 0, len(history))
	for i, message := range history {
		if llm.IsMessageSource(message, llm.MessageSourcePlugin, llm.MessageFormInstructions, "") {
			if _, ok := activeContent[message.Content]; !ok || lastActive[message.Content] != i {
				continue
			}
		}
		out = append(out, message)
	}
	return out
}
