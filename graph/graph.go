package graph

import (
	"context"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// RouterFunc resolves programmatic branch decisions accessing state boundaries in runtime dynamically.
type RouterFunc func(ctx context.Context, state *core.AgentState) (string, error)

type Edge struct {
	FromID string
	ToID   string
	IsCond bool
	Router RouterFunc
}

// Graph acts as the read-only execution definition protecting engine runtime from accidental cyclic or mutation graph drifts.
type Graph struct {
	Nodes     map[string]interfaces.Node
	Edges     []Edge
	Entry     string
	Terminals map[string]bool
}

// Node returns the targeted node via lookups guaranteeing safe interface interaction bounds.
func (g *Graph) Node(id string) (interfaces.Node, bool) {
	n, ok := g.Nodes[id]
	return n, ok
}
