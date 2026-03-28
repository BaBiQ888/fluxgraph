package core

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestMessageValidate(t *testing.T) {
	t.Run("valid tool result", func(t *testing.T) {
		m := Message{
			Role: RoleTool,
			Parts: []Part{
				{Type: PartTypeToolResult, ToolResult: &ToolResultPart{}},
			},
		}
		assert.NoError(t, m.Validate())
	})

	t.Run("invalid tool result in assistant", func(t *testing.T) {
		m := Message{
			Role: RoleAssistant,
			Parts: []Part{
				{Type: PartTypeToolResult, ToolResult: &ToolResultPart{}},
			},
		}
		assert.ErrorIs(t, m.Validate(), ErrInvalidMessageParts)
	})

	t.Run("invalid text in tool message", func(t *testing.T) {
		m := Message{
			Role: RoleTool,
			Parts: []Part{
				{Type: PartTypeText, Text: "hello"},
			},
		}
		assert.ErrorIs(t, m.Validate(), ErrInvalidMessageParts)
	})

	t.Run("invalid tool call in user message", func(t *testing.T) {
		m := Message{
			Role: RoleUser,
			Parts: []Part{
				{Type: PartTypeToolCall, ToolCall: &ToolCallPart{}},
			},
		}
		assert.ErrorIs(t, m.Validate(), ErrInvalidMessageParts)
	})
}
