package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/interfaces"
)

var (
	ErrMaxStepsExceeded = errors.New("max steps exceeded")
	ErrSessionNotFound  = errors.New("session state not found")
	ErrExecutionFailed  = errors.New("execution failed")
	ErrNoNextNode       = errors.New("no next node found for execution, and current node is not marked as Terminal")
)

type Engine struct {
	graph       *graph.Graph
	memoryStore interfaces.MemoryStore
	taskStore   interfaces.TaskStore
	eventBus    interfaces.EventBus
	hooks       []LifecycleHook
	retry       RetryPolicy
	maxSteps    int
}

type EngineOptions func(*Engine)

func WithMaxSteps(steps int) EngineOptions {
	return func(e *Engine) { e.maxSteps = steps }
}

func WithHooks(hooks ...LifecycleHook) EngineOptions {
	return func(e *Engine) { e.hooks = append(e.hooks, hooks...) }
}

func WithRetryPolicy(rp RetryPolicy) EngineOptions {
	return func(e *Engine) { e.retry = rp }
}

func WithTaskStore(ts interfaces.TaskStore) EngineOptions {
	return func(e *Engine) { e.taskStore = ts }
}

func NewEngine(g *graph.Graph, mem interfaces.MemoryStore, bus interfaces.EventBus, opts ...EngineOptions) *Engine {
	e := &Engine{
		graph:       g,
		memoryStore: mem,
		eventBus:    bus,
		maxSteps:    100,
		retry:       DefaultRetryPolicy(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// fireHooks dispatches to all hooks; panics inside hooks are recovered and logged without stopping the main loop.
func (e *Engine) fireHooks(state *core.AgentState, meta HookMeta) {
	for _, h := range e.hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Hook panicked — isolate and continue.
					_ = fmt.Sprintf("hook panic recovered: %v", r)
				}
			}()
			h.OnHook(state, meta)
		}()
	}
}

// Start initiates a brand new EventLoop flow.
func (e *Engine) Start(ctx context.Context, sessionID string, initialState *core.AgentState) (*core.AgentState, error) {
	state := initialState
	if state == nil {
		state = core.NewState()
	}
	state = state.WithStatus(core.StatusRunning)
	if state.LastNodeID == "" {
		state.LastNodeID = e.graph.Entry
	}
	return e.runLoop(ctx, sessionID, state)
}

// Resume bridges Human-In-The-Loop callbacks resolving active paused boundaries.
func (e *Engine) Resume(ctx context.Context, sessionID string, injection map[string]any) (*core.AgentState, error) {
	state, err := e.memoryStore.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if state.Status != core.StatusPaused {
		return state, nil
	}
	for k, v := range injection {
		state = state.WithVariable(k, v)
	}
	state = state.WithStatus(core.StatusRunning)

	nextNodeID, err := e.resolveNextNode(ctx, state)
	if err != nil {
		return nil, err
	}
	if nextNodeID == "" {
		if e.graph.Terminals[state.LastNodeID] {
			state = state.WithStatus(core.StatusCompleted)
			_, _ = e.memoryStore.Save(ctx, sessionID, state)
			return state, nil
		}
		return nil, ErrNoNextNode
	}
	state.LastNodeID = nextNodeID
	return e.runLoop(ctx, sessionID, state)
}

func (e *Engine) resolveNextNode(ctx context.Context, state *core.AgentState) (string, error) {
	for _, edge := range e.graph.Edges {
		if edge.FromID == state.LastNodeID {
			if edge.IsCond {
				return edge.Router(ctx, state)
			}
			return edge.ToID, nil
		}
	}
	return "", nil
}

func (e *Engine) processNodeWithRetry(ctx context.Context, node interfaces.Node, state *core.AgentState) (*interfaces.NodeResult, error) {
	var result *interfaces.NodeResult
	err := e.retry.Execute(ctx, func() error {
		r, err := node.Process(ctx, state)
		if err != nil {
			return err
		}
		result = r
		return nil
	})
	return result, err
}

func (e *Engine) runLoop(ctx context.Context, sessionID string, currentState *core.AgentState) (*core.AgentState, error) {
	state := currentState
	
	// Inject session_id into context for tools to extract
	ctx = context.WithValue(ctx, core.SessionContextKey, sessionID)

	for {
		if state.Status == core.StatusFailed || state.Status == core.StatusCompleted {
			_, err := e.memoryStore.Save(ctx, sessionID, state)
			return state, err
		}
		if state.StepCount >= e.maxSteps {
			state = state.WithStatus(core.StatusFailed)
			_, _ = e.memoryStore.Save(ctx, sessionID, state)
			return state, ErrMaxStepsExceeded
		}

		currentNodeID := state.LastNodeID
		node, exists := e.graph.Node(currentNodeID)
		if !exists {
			state = state.WithStatus(core.StatusFailed)
			_, _ = e.memoryStore.Save(ctx, sessionID, state)
			return state, ErrNoNextNode
		}

		// BeforeNode hook.
		e.fireHooks(state, HookMeta{Point: HookBeforeNode, NodeID: currentNodeID, StepCount: state.StepCount})
		nodeStart := time.Now()

		result, err := e.processNodeWithRetry(ctx, node, state)
		elapsed := time.Since(nodeStart)
		state.StepCount++

		if err != nil {
			e.fireHooks(state, HookMeta{Point: HookOnError, NodeID: currentNodeID, StepCount: state.StepCount, Elapsed: elapsed, Err: err})

			// Update Task status on failure (if TaskID exists)
			if state.TaskID != "" && e.taskStore != nil {
				_ = e.taskStore.UpdateStatus(ctx, state.TaskID, core.TaskStatus{
					State:     core.TaskStateFailed,
					Timestamp: time.Now(),
					Message:   err.Error(),
				})
			}

			// HumanNeeded → convert to Interrupt instead of failure.
			var ae *core.AgentError
			if errors.As(err, &ae) && ae.Category == core.ErrCategoryHumanNeeded {
				state = state.WithStatus(core.StatusPaused)
				_, _ = e.memoryStore.Save(ctx, sessionID, state)
				if e.eventBus != nil {
					_ = e.eventBus.Publish(interfaces.Event{
						Type:      interfaces.EventAgentPaused,
						SessionID: sessionID,
						Payload:   map[string]any{"reason": ae.Cause.Error()},
					})
				}
				return state, nil
			}

			state = state.WithStatus(core.StatusFailed)
			_, _ = e.memoryStore.Save(ctx, sessionID, state)
			return state, err
		}

		// AfterNode hook.
		e.fireHooks(result.State, HookMeta{Point: HookAfterNode, NodeID: currentNodeID, StepCount: state.StepCount, Elapsed: elapsed})

		if result.State != nil {
			result.State.StepCount = state.StepCount
			result.State.LastNodeID = currentNodeID
			
			// Sync new artifacts to TaskStore
			if result.State.TaskID != "" && e.taskStore != nil {
				// Only append new artifacts discovered in this step
				newArtifacts := result.State.Artifacts[len(state.Artifacts):]
				for _, a := range newArtifacts {
					_ = e.taskStore.AppendArtifact(ctx, result.State.TaskID, a)
				}
				
				// Update task status to reflect progression
				_ = e.taskStore.UpdateStatus(ctx, result.State.TaskID, core.TaskStatus{
					State:     core.MapAgentStatusToTaskState(result.State.Status),
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Completed node %s", currentNodeID),
				})
			}
			
			state = result.State
		}

		if result.Interrupt != nil {
			state = state.WithStatus(core.StatusPaused)
			_, _ = e.memoryStore.Save(ctx, sessionID, state)
			if e.eventBus != nil {
				_ = e.eventBus.Publish(interfaces.Event{
					Type:      interfaces.EventAgentPaused,
					SessionID: sessionID,
					Payload:   result.Interrupt.Payload,
				})
			}
			return state, nil
		}

		var nextNodeID string
		if len(result.NextNodes) > 0 {
			nextNodeID = result.NextNodes[0]
		} else {
			nl, rErr := e.resolveNextNode(ctx, state)
			if rErr != nil {
				state = state.WithStatus(core.StatusFailed)
				_, _ = e.memoryStore.Save(ctx, sessionID, state)
				return state, rErr
			}
			nextNodeID = nl
		}

		if nextNodeID == "" {
			if e.graph.Terminals[currentNodeID] {
				state = state.WithStatus(core.StatusCompleted)
				_, _ = e.memoryStore.Save(ctx, sessionID, state)
				return state, nil
			}
			state = state.WithStatus(core.StatusFailed)
			_, _ = e.memoryStore.Save(ctx, sessionID, state)
			return state, ErrNoNextNode
		}

		state.LastNodeID = nextNodeID
	}
}
