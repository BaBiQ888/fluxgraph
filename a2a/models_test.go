package a2a_test

import (
	"testing"

	"github.com/FluxGraph/fluxgraph/a2a"
	"github.com/FluxGraph/fluxgraph/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgentCard(t *testing.T) {
	reg := tools.NewConcreteToolRegistry()
	_ = reg.Register(&tools.EchoTool{})

	caps := a2a.AgentCapabilities{
		Streaming: true,
	}

	card := a2a.NewAgentCard(
		"TestAgent",
		"Test Desc",
		"1.0.0",
		"http://local",
		reg,
		caps,
	)

	assert.Equal(t, "TestAgent", card.Name)
	assert.Equal(t, "1.0", card.ProtocolVersion)
	require.Len(t, card.Skills, 1)
	assert.Equal(t, "echo", card.Skills[0].Name)
}
