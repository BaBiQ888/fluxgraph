package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/FluxGraph/fluxgraph/interfaces"
)

// EchoTool returns its input unchanged — useful for validating tool-call pipelines.
type EchoTool struct{}

func (e *EchoTool) Name() string        { return "echo" }
func (e *EchoTool) Description() string { return "Echoes the input message back to the caller." }
func (e *EchoTool) InputSchema() interfaces.ToolInputSchema {
	return interfaces.ToolInputSchema{
		Type: "object",
		Properties: map[string]map[string]any{
			"message": {"type": "string", "description": "The message to echo"},
		},
		Required: []string{"message"},
	}
}
func (e *EchoTool) RequiredPermissions() []string { return nil }
func (e *EchoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if msg, ok := args["message"].(string); ok {
		return msg, nil
	}
	return "", fmt.Errorf("missing or non-string 'message' argument")
}

// SleepTool simulates a slow operation — verifies concurrent execution and timeout handling.
type SleepTool struct{}

func (s *SleepTool) Name() string        { return "sleep" }
func (s *SleepTool) Description() string { return "Sleeps for the given number of milliseconds." }
func (s *SleepTool) InputSchema() interfaces.ToolInputSchema {
	return interfaces.ToolInputSchema{
		Type: "object",
		Properties: map[string]map[string]any{
			"ms": {"type": "integer", "description": "Milliseconds to sleep"},
		},
		Required: []string{"ms"},
	}
}
func (s *SleepTool) RequiredPermissions() []string { return nil }
func (s *SleepTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	msRaw, ok := args["ms"]
	if !ok {
		return "", fmt.Errorf("missing 'ms' argument")
	}
	var dur time.Duration
	switch v := msRaw.(type) {
	case float64:
		dur = time.Duration(v) * time.Millisecond
	case int:
		dur = time.Duration(v) * time.Millisecond
	default:
		return "", fmt.Errorf("'ms' must be a number, got %T", msRaw)
	}

	select {
	case <-time.After(dur):
		return fmt.Sprintf("slept %v", dur), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
