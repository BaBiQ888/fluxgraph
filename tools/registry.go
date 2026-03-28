package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

// ConcreteToolRegistry provides a production-grade thread-safe tool store with
// tenant-scoped permissions, pre-dispatch auth checks, and goroutine-based
// concurrent execution maintaining original call ordering.
type ConcreteToolRegistry struct {
	mu          sync.RWMutex
	tools       map[string]interfaces.Tool
	permissions map[string]map[string]bool // tenantID → set of allowed tool names
}

func NewConcreteToolRegistry() *ConcreteToolRegistry {
	return &ConcreteToolRegistry{
		tools:       make(map[string]interfaces.Tool),
		permissions: make(map[string]map[string]bool),
	}
}

// Register adds a tool; returns ErrNodeAlreadyExists-style error on collision.
func (r *ConcreteToolRegistry) Register(tool interfaces.Tool, permissions ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name()]; exists {
		return fmt.Errorf("tool already registered: %s", tool.Name())
	}
	r.tools[tool.Name()] = tool
	return nil
}

func (r *ConcreteToolRegistry) GetTool(name string) (interfaces.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *ConcreteToolRegistry) ListTools() []interfaces.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]interfaces.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// GrantPermission allows a specific tenantID to call the named tool.
func (r *ConcreteToolRegistry) GrantPermission(tenantID, toolName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.permissions[tenantID]; !ok {
		r.permissions[tenantID] = make(map[string]bool)
	}
	r.permissions[tenantID][toolName] = true
}

// RevokePermission removes a tenantID's access to a tool.
func (r *ConcreteToolRegistry) RevokePermission(tenantID, toolName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.permissions[tenantID]; ok {
		delete(m, toolName)
	}
}

// AuthorizeCall checks if the tenantID embedded in ctx may call the tool.
// The empty tenantID "" is treated as unrestricted (no tenant isolation configured).
func (r *ConcreteToolRegistry) AuthorizeCall(ctx context.Context, toolName string) error {
	tenantID, _ := ctx.Value("tenantID").(string)
	if tenantID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	allowed, ok := r.permissions[tenantID]
	if !ok {
		return fmt.Errorf("tenant %s has no permission table; access to %s denied", tenantID, toolName)
	}

	// 1. Check exact match
	if allowed[toolName] {
		return nil
	}

	// 2. Check full wildcard
	if allowed["*"] {
		return nil
	}

	// 3. Check prefix wildcard (e.g. "filesystem:*")
	for pattern := range allowed {
		if strings.HasSuffix(pattern, ":*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(toolName, prefix) {
				return nil
			}
		}
	}

	return fmt.Errorf("tenant %s is not authorized to call tool %s", tenantID, toolName)
}

// ExecuteConcurrent dispatches every ToolCall in a goroutine, waits via
// WaitGroup, and returns results preserving original index order.
func (r *ConcreteToolRegistry) ExecuteConcurrent(ctx context.Context, calls []core.ToolCallPart) []core.ToolResultPart {
	results := make([]core.ToolResultPart, len(calls))
	var wg sync.WaitGroup

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, tc core.ToolCallPart) {
			defer wg.Done()

			// Auth check before execution.
			if err := r.AuthorizeCall(ctx, tc.ToolName); err != nil {
				results[idx] = core.ToolResultPart{
					CallID:  tc.CallID,
					Result:  fmt.Sprintf("PermissionDenied: %v", err),
					IsError: true,
				}
				return
			}

			r.mu.RLock()
			tool, ok := r.tools[tc.ToolName]
			r.mu.RUnlock()
			if !ok {
				results[idx] = core.ToolResultPart{
					CallID:  tc.CallID,
					Result:  fmt.Sprintf("tool not found: %s", tc.ToolName),
					IsError: true,
				}
				return
			}

			// Each goroutine gets its own child context so a single timeout
			// doesn't cancel the rest.
			childCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			out, err := tool.Execute(childCtx, tc.Arguments)
			if err != nil {
				results[idx] = core.ToolResultPart{CallID: tc.CallID, Result: err.Error(), IsError: true}
			} else {
				results[idx] = core.ToolResultPart{CallID: tc.CallID, Result: out, IsError: false}
			}
		}(i, call)
	}

	wg.Wait()
	return results
}
