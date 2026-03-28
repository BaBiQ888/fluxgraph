package eval

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"gopkg.in/yaml.v3"
)

// TestCase defines a single evaluation scenario.
type TestCase struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Input       string            `yaml:"input"`
	Expected    []Assertion       `yaml:"expected"`
	Variables   map[string]any    `yaml:"variables"`
}

// Assertion defines a check to perform on the final state.
type Assertion struct {
	Type      string  `yaml:"type"`      // "contains", "not_contains", "status_is", "variable_equals"
	Key       string  `yaml:"key"`       // Variable key if type is variable_equals
	Value     string  `yaml:"value"`     // Expected value or substring
	Threshold float64 `yaml:"threshold"` // For performance benchmarks
}

// EvalHarness runs a suite of test cases against the engine.
type EvalHarness struct {
	engine *engine.Engine
}

func NewEvalHarness(eng *engine.Engine) *EvalHarness {
	return &EvalHarness{engine: eng}
}

// RunSuite loads scenarios from a YAML file and executes them.
func (h *EvalHarness) RunSuite(ctx context.Context, filePath string) ([]Result, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var suite []TestCase
	if err := yaml.Unmarshal(data, &suite); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(suite))
	for _, tc := range suite {
		res := h.RunTestCase(ctx, tc)
		results = append(results, res)
	}
	return results, nil
}

// Result captures the outcome of a single test case.
type Result struct {
	Name    string
	Passed  bool
	Error   string
	Metrics map[string]any
}

func (h *EvalHarness) RunTestCase(ctx context.Context, tc TestCase) Result {
	state := core.NewState()
	for k, v := range tc.Variables {
		state = state.WithVariable(k, v)
	}
	state.TaskID = fmt.Sprintf("eval-%s", tc.Name)
	state.ContextID = "eval-context"
	
	state = state.WithMessage(core.Message{
		Role:  core.RoleUser,
		Parts: []core.Part{{Type: core.PartTypeText, Text: tc.Input}},
	})

	sessionID := state.TaskID
	
	// Inject tenant_id into context if present in variables
	runCtx := ctx
	if tid, ok := tc.Variables["tenant_id"].(string); ok {
		runCtx = context.WithValue(ctx, "tenantID", tid)
	}

	finalState, err := h.engine.Start(runCtx, sessionID, state)

	res := Result{Name: tc.Name, Passed: true}
	if err != nil {
		// Even if error occurs, check if it was expected (e.g. sanitization rejection)
		res.Passed = false
		res.Error = err.Error()
	}

	// Run Assertions
	for _, as := range tc.Expected {
		if !h.checkAssertion(finalState, err, as) {
			res.Passed = false
			res.Error += fmt.Sprintf("Assertion failed: %s %s %s; ", as.Type, as.Key, as.Value)
		} else {
			// If assertion passed but error was present, we might still count as passed 
			// if the test EXPECTED an error (e.g. status_is Failed).
			if as.Type == "status_is" && as.Value == "Failed" && err != nil {
				res.Passed = true
				res.Error = ""
			}
		}
	}

	return res
}

func (h *EvalHarness) checkAssertion(state *core.AgentState, err error, as Assertion) bool {
	if state == nil {
		// If engine failed before returning state, we can only check for status_is Failed
		if as.Type == "status_is" && as.Value == "Failed" {
			return err != nil
		}
		return false
	}

	switch as.Type {
	case "status_is":
		return string(state.Status) == as.Value
	case "variable_equals":
		v, err := state.GetStringVariable(as.Key)
		if err != nil {
			return false
		}
		return v == as.Value
	case "contains":
		for _, msg := range state.Messages {
			if msg.Role == core.RoleAssistant {
				for _, part := range msg.Parts {
					if part.Type == core.PartTypeText && strings.Contains(part.Text, as.Value) {
						return true
					}
				}
			}
		}
		return false
	case "not_contains":
		for _, msg := range state.Messages {
			if msg.Role == core.RoleAssistant {
				for _, part := range msg.Parts {
					if part.Type == core.PartTypeText && strings.Contains(part.Text, as.Value) {
						return false
					}
				}
			}
		}
		return true
	}
	return false
}
