package a2a

import (
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// AgentCard represents the standardized A2A capability declaration.
type AgentCard struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Version         string            `json:"version"`         // Semantic version
	URL             string            `json:"url"`             // A2A service endpoint
	ProtocolVersion string            `json:"protocolVersion"` // Always "1.0" for FluxGraph
	Capabilities    AgentCapabilities `json:"capabilities"`
	Skills          []AgentSkill      `json:"skills"`
	Security        []SecurityScheme  `json:"security,omitempty"`
}

// AgentCapabilities defines boolean flags for A2A features.
type AgentCapabilities struct {
	Streaming           bool `json:"streaming"`
	PushNotifications   bool `json:"pushNotifications"`
	ExtendedAgentCard   bool `json:"extendedAgentCard"`
	StateTimeTravel     bool `json:"stateTimeTravel"` // FluxGraph specific
	MultiTenancy        bool `json:"multiTenancy"`    // FluxGraph specific
}

// AgentSkill represents a tool mapped to a natural language capability.
type AgentSkill struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	InputSchema interfaces.ToolInputSchema `json:"inputSchema"`
	InputModes  []string                   `json:"inputModes"`  // e.g., ["text", "structured_data"]
	OutputModes []string                   `json:"outputModes"` // e.g., ["text"]
	Tags        []string                   `json:"tags,omitempty"`
}

// SecurityScheme defines how to authenticate with this agent.
type SecurityScheme struct {
	Type             string `json:"type"` // "bearer" | "oauth2" | "apiKey"
	AuthURL          string `json:"authUrl,omitempty"`
	TokenURL         string `json:"tokenUrl,omitempty"`
	VerificationMode string `json:"verificationMode,omitempty"`
}

// NewAgentCard constructs a card using registered tools and engine flags.
func NewAgentCard(
	name, desc, version, url string,
	registry interfaces.ToolRegistry,
	caps AgentCapabilities,
) *AgentCard {
	card := &AgentCard{
		Name:            name,
		Description:     desc,
		Version:         version,
		URL:             url,
		ProtocolVersion: "1.0",
		Capabilities:    caps,
		Skills:          make([]AgentSkill, 0),
	}

	for _, t := range registry.ListTools() {
		skill := AgentSkill{
			ID:          t.Name(),
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
			InputModes:  []string{"text", "structured_data"},
			OutputModes: []string{"text"},
		}
		card.Skills = append(card.Skills, skill)
	}

	return card
}
