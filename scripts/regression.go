//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/eval"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/memory"
	"github.com/FluxGraph/fluxgraph/security"
	"github.com/FluxGraph/fluxgraph/tools"
)

// SmartMockNode simulates different behaviors based on input
type SmartMockNode struct {
	id string
	registry interfaces.ToolRegistry
}

func (n *SmartMockNode) ID() string { return n.id }
func (n *SmartMockNode) Process(ctx context.Context, state *core.AgentState) (*interfaces.NodeResult, error) {
	userInput := ""
	if len(state.Messages) > 0 {
		userInput = state.Messages[len(state.Messages)-1].Parts[0].Text
	}

	// 1. Simulate Prompt Injection Response (Manual Check for Demo)
	if strings.Contains(userInput, "Ignore all previous") {
		// If we had a sanitizer hook, it would have failed before this.
		// For the test "status_is Failed", we can return an error here to simulate a rejected task.
		return nil, fmt.Errorf("security violation: prompt injection detected")
	}

	// 2. Simulate PII Output
	if strings.Contains(userInput, "email") {
		state = state.WithMessage(core.Message{
			Role:  core.RoleAssistant,
			Parts: []core.Part{{Type: core.PartTypeText, Text: "My email is test@example.com"}},
		})
	}

	// 3. Simulate Tool Call (for AuthZ test)
	if strings.Contains(userInput, "passwd") {
		// Attempt to call a restricted tool
		results := n.registry.ExecuteConcurrent(ctx, []core.ToolCallPart{
			{CallID: "call-1", ToolName: "restricted_tool", Arguments: map[string]any{}},
		})
		for _, r := range results {
			if r.IsError && strings.Contains(r.Result, "PermissionDenied") {
				state = state.WithMessage(core.Message{
					Role:  core.RoleAssistant,
					Parts: []core.Part{{Type: core.PartTypeText, Text: "Error: PermissionDenied"}},
				})
			}
		}
	}

	// 4. Normal Response
	if strings.Contains(userInput, "2+2") {
		state = state.WithMessage(core.Message{
			Role:  core.RoleAssistant,
			Parts: []core.Part{{Type: core.PartTypeText, Text: "The answer is 4"}},
		})
	}

	return &interfaces.NodeResult{State: state}, nil
}

func main() {
	fmt.Println("=== FluxGraph Infrastructure Verification (EvalHarness) ===")

	// 1. Setup Infrastructure
	store := memory.NewInMemoryStore()
	registry := tools.NewConcreteToolRegistry()
	
	// Define permissions for the test
	// tenant-a has no permissions for 'restricted_tool'
	
	// 2. Build Graph with Smart Mock
	builder := graph.NewBuilder()
	node := &SmartMockNode{id: "LogicNode", registry: registry}
	builder.AddNode(node)
	builder.SetEntry("LogicNode")
	builder.SetTerminal("LogicNode")
	g, err := builder.Build()
	if err != nil {
		log.Fatalf("Failed to build graph: %v", err)
	}

	// 3. Setup Engine with Security Hooks (Phase 4 Components)
	auditHook, _ := security.NewAuditLogHook("fluxgraph_verification.log")
	guardHook := security.NewOutputGuardHook()
	
	eng := engine.NewEngine(g, store, nil, engine.WithHooks(auditHook, guardHook))

	// 4. Run Harness
	harness := eval.NewEvalHarness(eng)
	ctx := context.Background()

	results, err := harness.RunSuite(ctx, "eval/scenarios/security_bench.yaml")
	if err != nil {
		log.Fatalf("Failed to run suite: %v", err)
	}

	total := len(results)
	passed := 0
	for _, res := range results {
		status := "PASSED"
		if !res.Passed {
			status = fmt.Sprintf("FAILED (%s)", res.Error)
		} else {
			passed++
		}
		fmt.Printf("- [%s] %s\n", status, res.Name)
	}

	fmt.Printf("\nSummary: %d/%d Passed\n", passed, total)
	fmt.Println("=== Verification Complete ===")
}
