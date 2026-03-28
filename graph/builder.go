package graph

import (
	"errors"
	"fmt"
	"log"

	"github.com/FluxGraph/fluxgraph/interfaces"
)

var (
	ErrNodeAlreadyExists = errors.New("node already exists")
	ErrNodeNotFound      = errors.New("node not found")
	ErrEntryNotSet       = errors.New("entry node not set")
	ErrGraphValidation   = errors.New("graph validation failed")
)

type Builder struct {
	nodes     map[string]interfaces.Node
	edges     []Edge
	entry     string
	terminals map[string]bool
}

func NewBuilder() *Builder {
	return &Builder{
		nodes:     make(map[string]interfaces.Node),
		terminals: make(map[string]bool),
	}
}

func (b *Builder) AddNode(node interfaces.Node) error {
	id := node.ID()
	if _, exists := b.nodes[id]; exists {
		return fmt.Errorf("%w: %s", ErrNodeAlreadyExists, id)
	}
	b.nodes[id] = node
	return nil
}

func (b *Builder) SetEntry(nodeID string) {
	b.entry = nodeID
}

func (b *Builder) SetTerminal(nodeIDs ...string) {
	for _, id := range nodeIDs {
		b.terminals[id] = true
	}
}

func (b *Builder) AddEdge(fromID, toID string) {
	b.edges = append(b.edges, Edge{
		FromID: fromID,
		ToID:   toID,
		IsCond: false,
	})
}

func (b *Builder) AddConditionalEdge(fromID string, router RouterFunc) {
	b.edges = append(b.edges, Edge{
		FromID: fromID,
		IsCond: true,
		Router: router,
	})
}

func (b *Builder) checkReachability() {
	reachable := make(map[string]bool)
	queue := []string{b.entry}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if reachable[curr] {
			continue
		}
		reachable[curr] = true

		for _, e := range b.edges {
			if e.FromID == curr {
				if !e.IsCond {
					queue = append(queue, e.ToID)
				}
				// We can't strictly prove cond edge reachability beforehand.
			}
		}
	}

	for id := range b.nodes {
		if !reachable[id] {
			// Phase 1: Output terminal logger traces for isolated artifacts logic visually indicating unreachable pathways.
			log.Printf("[Warning] node %s may be unreachable from entry %s", id, b.entry)
		}
	}
}

// Build finalizes topology construction, scanning graph validity checks ensuring no broken execution traces.
func (b *Builder) Build() (*Graph, error) {
	if b.entry == "" {
		return nil, ErrEntryNotSet
	}

	if _, ok := b.nodes[b.entry]; !ok {
		return nil, fmt.Errorf("%w: entry node %s not registered", ErrGraphValidation, b.entry)
	}

	// Reference check preventing panic in Engine Loop bounds.
	for _, edge := range b.edges {
		if _, ok := b.nodes[edge.FromID]; !ok {
			return nil, fmt.Errorf("%w: edge references missing from node %s", ErrGraphValidation, edge.FromID)
		}
		if !edge.IsCond {
			if _, ok := b.nodes[edge.ToID]; !ok {
				return nil, fmt.Errorf("%w: static edge references missing to node %s", ErrGraphValidation, edge.ToID)
			}
		}
	}

	for t := range b.terminals {
		if _, ok := b.nodes[t]; !ok {
			return nil, fmt.Errorf("%w: terminal node %s not registered", ErrGraphValidation, t)
		}
	}

	b.checkReachability()

	// Immutable conversion copy.
	g := &Graph{
		Nodes:     make(map[string]interfaces.Node, len(b.nodes)),
		Edges:     make([]Edge, len(b.edges)),
		Entry:     b.entry,
		Terminals: make(map[string]bool, len(b.terminals)),
	}

	for k, v := range b.nodes {
		g.Nodes[k] = v
	}
	copy(g.Edges, b.edges)
	for k, v := range b.terminals {
		g.Terminals[k] = v
	}

	return g, nil
}
