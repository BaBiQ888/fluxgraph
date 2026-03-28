package graph

import (
	"context"
	"testing"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/stretchr/testify/assert"
)

type testNode struct {
	id string
}

func (n testNode) ID() string { return n.id }
func (n testNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	return &interfaces.NodeResult{State: state}, nil
}

func TestGraphBuilder(t *testing.T) {
	t.Run("successful build", func(t *testing.T) {
		b := NewBuilder()
		_ = b.AddNode(testNode{"A"})
		_ = b.AddNode(testNode{"B"})
		b.SetEntry("A")
		b.SetTerminal("B")
		b.AddEdge("A", "B")

		g, err := b.Build()
		assert.NoError(t, err)
		assert.NotNil(t, g)
		assert.Equal(t, "A", g.Entry)
		
		node, ok := g.Node("B")
		assert.True(t, ok)
		assert.Equal(t, "B", node.ID())
	})

	t.Run("missing entry node", func(t *testing.T) {
		b := NewBuilder()
		b.SetEntry("A")

		_, err := b.Build()
		assert.ErrorIs(t, err, ErrGraphValidation)
	})

	t.Run("missing edge target", func(t *testing.T) {
		b := NewBuilder()
		_ = b.AddNode(testNode{"A"})
		b.SetEntry("A")
		b.AddEdge("A", "C") // C does not exist

		_, err := b.Build()
		assert.ErrorIs(t, err, ErrGraphValidation)
	})
	
	t.Run("entry not set at all", func(t *testing.T) {
		b := NewBuilder()
		_ = b.AddNode(testNode{"A"})
		
		_, err := b.Build()
		assert.ErrorIs(t, err, ErrEntryNotSet)
	})
	
	t.Run("terminal node validation", func(t *testing.T) {
		b := NewBuilder()
		_ = b.AddNode(testNode{"A"})
		b.SetEntry("A")
		b.SetTerminal("B")
		
		_, err := b.Build()
		assert.ErrorIs(t, err, ErrGraphValidation)
	})
}
