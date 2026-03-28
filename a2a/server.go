package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/FluxGraph/fluxgraph/observability"
	"github.com/FluxGraph/fluxgraph/security"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/propagation"
)

// Server provides the HTTP endpoint for A2A communication.
type Server struct {
	router      *chi.Mux
	engine      *engine.Engine
	taskStore   interfaces.TaskStore
	eventBus    interfaces.EventBus
	registry    interfaces.ToolRegistry
	auth        *Authenticator
	card        *AgentCard
	inspector   *observability.StateInspector
	audit       *security.AuditLogHook
	logger      zerolog.Logger
}

type ServerOptions struct {
	Name        string
	Description string
	Version     string
	URL         string
	Secret      string
}

func NewServer(eng *engine.Engine, ts interfaces.TaskStore, mem interfaces.MemoryStore, reg interfaces.ToolRegistry, bus interfaces.EventBus, opts ServerOptions) *Server {
	auth := NewAuthenticator(opts.Secret)
	
	// Pre-generate the public AgentCard.
	card := NewAgentCard(opts.Name, opts.Description, opts.Version, opts.URL, reg, AgentCapabilities{
		Streaming:         true, // Default supported
		StateTimeTravel:   true,
		MultiTenancy:      true,
	})

	s := &Server{
		router:    chi.NewRouter(),
		engine:    eng,
		taskStore: ts,
		eventBus:  bus,
		registry:  reg,
		auth:      auth,
		card:      card,
		inspector: observability.NewStateInspector(mem),
		logger:    log.With().Str("component", "a2a-server").Logger(),
	}

	audit, _ := security.NewAuditLogHook("fluxgraph_security.log")
	s.audit = audit

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	// Public endpoints
	s.router.Get("/.well-known/agent.json", s.handleAgentCard)
	s.router.Handle("/metrics", observability.MetricsHandler())

	// Authenticated endpoints
	s.router.Group(func(r chi.Router) {
		r.Use(s.auth.Middleware)

		r.Post("/", s.handleJSONRPC)
		r.Get("/tasks/{taskID}", s.handleGetTask)
		r.Get("/tasks/{taskID}/events", s.handleTaskEvents)
		r.Post("/tasks/{taskID}/cancel", s.handleCancelTask)

		// Debug endpoints (high privilege)
		r.Get("/debug/sessions/{taskID}/timeline", s.handleGetTimeline)
		r.Get("/debug/sessions/{taskID}/diff", s.handleDiffCheckpoints)
		r.Post("/debug/checkpoints/{checkpointID}/replay", s.handleReplayFrom)
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// ---- Handlers ----

func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.card)
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	// Extract TraceContext from headers
	ctx := observability.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	r = r.WithContext(ctx)

	var req RPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendRPCError(w, nil, RPCCodeParseError, "Parse error", nil)
		return
	}

	if req.JSONRPC != "2.0" {
		s.sendRPCError(w, req.ID, RPCCodeInvalidRequest, "Invalid Request", nil)
		return
	}

	switch req.Method {
	case "message/send":
		s.handleSendMessage(w, r, req)
	case "tasks/get":
		s.handleGetTaskRPC(w, r, req)
	case "tasks/pushNotificationConfig/create":
		s.handleCreatePushConfig(w, r, req)
	default:
		s.sendRPCError(w, req.ID, RPCCodeMethodNotFound, "Method not found", nil)
	}
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params SendMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendRPCError(w, req.ID, RPCCodeInvalidParams, "Invalid params", nil)
		return
	}

	tenantID, _ := r.Context().Value("tenantID").(string)
	if tenantID == "" { tenantID = "default" }

	// Input Sanitization (Module 31)
	if err := security.SanitizeMessage(params.Message); err != nil {
		if s.audit != nil {
			s.audit.LogViolation(tenantID, params.ContextID, "INPUT_SANITIZATION_FAILED", err.Error())
		}
		s.sendRPCError(w, req.ID, RPCCodeInvalidParams, err.Error(), nil)
		return
	}
	if !HasScope(r.Context(), "agent:write") {
		s.sendRPCError(w, req.ID, RPCCodeUnauthorized, "Insufficient scope", nil)
		return
	}

	taskID := params.TaskID
	var existingTask *core.Task
	if taskID != "" {
		existingTask, _ = s.taskStore.GetByID(r.Context(), taskID)
	}

	if existingTask != nil && existingTask.Status.State == core.TaskStateInputRequired {
		// Resume existing task
		existingTask.History = append(existingTask.History, params.Message)
		existingTask.Status.State = core.TaskStateWorking
		existingTask.UpdatedAt = time.Now()
		
		_ = s.taskStore.UpdateStatus(r.Context(), taskID, existingTask.Status)
		_ = s.taskStore.AppendMessage(r.Context(), taskID, params.Message)

		// Resume engine execution (Logic for actual resume signal to engine would go here)
		// For now we simulate by starting fresh but with context
		go func() {
			_, _ = s.engine.Start(context.Background(), taskID, nil) // Resume would use real signal
		}()
		
		s.sendRPCResult(w, req.ID, existingTask)
		return
	}

	if taskID == "" {
		taskID = uuid.New().String()
	}
	contextID := params.ContextID
	if contextID == "" {
		contextID = uuid.New().String()
	}

	task := &core.Task{
		ID:        taskID,
		ContextID: contextID,
		TenantID:  tenantID,
		Status: core.TaskStatus{
			State:     core.TaskStateSubmitted,
			Timestamp: time.Now(),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Multi-turn context inheritance
	var history []core.Message
	if params.ContextID != "" {
		prevTasks, err := s.taskStore.ListByContextID(r.Context(), params.ContextID)
		if err == nil {
			for _, pt := range prevTasks {
				// Simplified: inherit all messages from previous tasks
				history = append(history, pt.History...)
			}
		}
	}
	task.History = history

	if err := s.taskStore.Create(r.Context(), task); err != nil {
		s.sendRPCError(w, req.ID, RPCCodeInternalError, "Failed to create task", nil)
		return
	}

	// Prepare initial AgentState for engine
	state := core.NewState()
	state.TaskID = taskID
	state.ContextID = contextID
	state.Messages = append(state.Messages, history...)
	state = state.WithMessage(params.Message)

	// Background execution
	go func() {
		_, _ = s.engine.Start(context.Background(), taskID, state)
	}()

	if params.ReturnImmediately {
		s.sendRPCResult(w, req.ID, task)
		return
	}

	// For simple sync behavior, we poll the task status (simplified for this module)
	// Real sync behavior would wait for engine completion. 
	// To keep it clean, we'll return "working" task and let client poll or use SSE.
	s.sendRPCResult(w, req.ID, task)
}

func (s *Server) handleGetTaskRPC(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendRPCError(w, req.ID, RPCCodeInvalidParams, "Invalid params", nil)
		return
	}

	task, err := s.taskStore.GetByID(r.Context(), params.TaskID)
	if err != nil {
		s.sendRPCError(w, req.ID, RPCCodeTaskNotFound, "Task not found", nil)
		return
	}
	s.sendRPCResult(w, req.ID, task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	task, err := s.taskStore.GetByID(r.Context(), taskID)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

func (s *Server) handleCreatePushConfig(w http.ResponseWriter, r *http.Request, req RPCRequest) {
	var params CreatePushConfigParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendRPCError(w, req.ID, RPCCodeInvalidParams, "Invalid params", nil)
		return
	}

	config := core.WebhookConfig{
		ID:         uuid.New().String(),
		URL:        params.URL,
		Secret:     params.Secret,
		EventTypes: params.EventTypes,
		Headers:    params.Headers,
	}

	if err := s.taskStore.AddWebhook(r.Context(), params.TaskID, config); err != nil {
		s.sendRPCError(w, req.ID, RPCCodeInternalError, "Failed to add webhook", nil)
		return
	}

	s.sendRPCResult(w, req.ID, config)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	// Task cancellation logic would involve e.engine signals.
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Channel for events belonging to this task
	eventChan := make(chan interfaces.Event, 10)
	
	// Subscription handler
	handler := func(ev interfaces.Event) {
		if ev.TaskID == taskID || ev.SessionID == taskID {
			eventChan <- ev
		}
	}

	// Subscribe to all relevant life-cycle events
	subID, _ := s.eventBus.Subscribe(interfaces.EventNodeCompleted, handler)
	defer func() { _ = s.eventBus.Unsubscribe(subID) }()
	
	subID2, _ := s.eventBus.Subscribe(interfaces.EventTaskCompleted, handler)
	defer func() { _ = s.eventBus.Unsubscribe(subID2) }()

	subID3, _ := s.eventBus.Subscribe(interfaces.EventAgentPaused, handler)
	defer func() { _ = s.eventBus.Unsubscribe(subID3) }()

	// Keep-alive ticker
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": keep-alive\n\n")
			flusher.Flush()
		case ev := <-eventChan:
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\n", ev.Type)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
			
			if ev.Type == interfaces.EventTaskCompleted {
				return
			}
		}
	}
}

func (s *Server) handleGetTimeline(w http.ResponseWriter, r *http.Request) {
	if !HasScope(r.Context(), "agent:debug") {
		http.Error(w, "Insufficient scope", http.StatusForbidden)
		return
	}
	taskID := chi.URLParam(r, "taskID")
	timeline, err := s.inspector.GetTimeline(r.Context(), taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(timeline)
}

func (s *Server) handleDiffCheckpoints(w http.ResponseWriter, r *http.Request) {
	if !HasScope(r.Context(), "agent:debug") {
		http.Error(w, "Insufficient scope", http.StatusForbidden)
		return
	}
	cpA := r.URL.Query().Get("cpA")
	cpB := r.URL.Query().Get("cpB")
	diff, err := s.inspector.DiffCheckpoints(r.Context(), cpA, cpB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(diff)
}

func (s *Server) handleReplayFrom(w http.ResponseWriter, r *http.Request) {
	if !HasScope(r.Context(), "agent:debug") {
		http.Error(w, "Insufficient scope", http.StatusForbidden)
		return
	}
	checkpointID := chi.URLParam(r, "checkpointID")
	state, err := s.inspector.ReplayFrom(r.Context(), checkpointID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tenantID, _ := state.GetStringVariable("tenant_id")
	if tenantID == "" {
		tenantID = "default"
	}

	// Start a new task for this replay
	taskID := uuid.New().String()
	task := &core.Task{
		ID:        taskID,
		ContextID: state.ContextID,
		TenantID:  tenantID,
		Status: core.TaskStatus{
			State:     core.TaskStateWorking,
			Timestamp: time.Now(),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		History:   state.Messages,
	}

	if err := s.taskStore.Create(r.Context(), task); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go func() {
		_, _ = s.engine.Start(context.Background(), taskID, state)
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

func (s *Server) sendRPCError(w http.ResponseWriter, id any, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

func (s *Server) sendRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}
