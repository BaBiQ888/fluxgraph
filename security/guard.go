package security

import (
	"regexp"
	"strings"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
)

var (
	// PIIPatterns for masking sensitive information in LLM output
	piiPatterns = map[string]*regexp.Regexp{
		"CREDIT_CARD": regexp.MustCompile(`(?:\d[ -]*?){13,16}`),
		"EMAIL":       regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),
		"IP_ADDRESS":  regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
	}
)

// OutputGuardHook masks sensitive data from LLM outputs before they are persisted.
type OutputGuardHook struct {
	// Add config for specific masks
}

func NewOutputGuardHook() *OutputGuardHook {
	return &OutputGuardHook{}
}

func (h *OutputGuardHook) OnHook(state *core.AgentState, meta engine.HookMeta) {
	if meta.Point != engine.HookAfterNode {
		return
	}

	// Scan the last message for PII if it's an assistant message
	if len(state.Messages) == 0 {
		return
	}
	lastMsg := &state.Messages[len(state.Messages)-1]
	if lastMsg.Role != core.RoleAssistant {
		return
	}

	for i := range lastMsg.Parts {
		part := &lastMsg.Parts[i]
		if part.Type == core.PartTypeText {
			part.Text = h.applyMask(part.Text)
		} else if part.Type == core.PartTypeToolResult && part.ToolResult != nil {
			part.ToolResult.Result = h.applyMask(part.ToolResult.Result)
		}
	}
}

func (h *OutputGuardHook) applyMask(text string) string {
	content := text
	for _, p := range piiPatterns {
		content = p.ReplaceAllStringFunc(content, func(match string) string {
			if len(match) <= 4 {
				return "****"
			}
			return match[:2] + strings.Repeat("*", len(match)-4) + match[len(match)-2:]
		})
	}
	return content
}

// MaskPII is a standalone utility for cleaning text.
func MaskPII(text string) string {
	content := text
	for _, p := range piiPatterns {
		content = p.ReplaceAllString(content, "****")
	}
	return content
}
