package core

import (
	"errors"
	"time"
)

type Role string

const (
	RoleUser      Role = "User"
	RoleAssistant Role = "Assistant"
	RoleSystem    Role = "System"
	RoleTool      Role = "Tool"
)

var ErrInvalidMessageParts = errors.New("invalid message parts for role")

// Message is the standard payload exchanged between LLMs and users.
type Message struct {
	ID        string
	Role      Role
	Parts     []Part
	Timestamp time.Time
	Metadata  map[string]any
	ContextID string // Reference to A2A ContextID (optional in Phase 1)
	TaskID    string // Reference to A2A TaskID (optional in Phase 1)
}

// Validate checks if the message parts are permissible based on its role.
func (m *Message) Validate() error {
	for _, part := range m.Parts {
		switch m.Role {
		case RoleTool:
			if part.Type != PartTypeToolResult {
				return ErrInvalidMessageParts
			}
		case RoleAssistant:
			if part.Type == PartTypeToolResult {
				return ErrInvalidMessageParts
			}
		case RoleSystem:
			if part.Type != PartTypeText {
				return ErrInvalidMessageParts
			}
		case RoleUser:
			if part.Type == PartTypeToolCall || part.Type == PartTypeToolResult {
				return ErrInvalidMessageParts
			}
		}
	}
	return nil
}
