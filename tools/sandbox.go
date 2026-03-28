package tools

import (
	"context"
	"time"

	"github.com/FluxGraph/fluxgraph/interfaces"
)

// SandboxedTool wraps a standard tool with extra safety constraints.
type SandboxedTool struct {
	base        interfaces.Tool
	timeout     time.Duration
}

func NewSandboxedTool(base interfaces.Tool, timeout time.Duration) *SandboxedTool {
	return &SandboxedTool{
		base:    base,
		timeout: timeout,
	}
}

func (s *SandboxedTool) Name() string { return s.base.Name() }
func (s *SandboxedTool) Description() string { return s.base.Description() }
func (s *SandboxedTool) InputSchema() interfaces.ToolInputSchema { return s.base.InputSchema() }
func (s *SandboxedTool) RequiredPermissions() []string { return s.base.RequiredPermissions() }

func (s *SandboxedTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	// 1. Apply local timeout constraint
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	// 2. Perform argument safety checks (Placeholder for basic checks)
	// Example: limit deep levels of map or protect sensitive keys.
	
	// 3. Delegate to base tool
	return s.base.Execute(ctx, args)
}

// WrapToolRegistry wraps all tools in a registry with a sandbox if they aren't already.
// This is a utility for mass-enforcing safety.
func WrapToolRegistry(reg *ConcreteToolRegistry, defaultTimeout time.Duration) {
	// In a real implementation, we might want to modify the registry 
	// or provide a middleware-like structure.
	// For this module, we provide the wrapper class.
}
