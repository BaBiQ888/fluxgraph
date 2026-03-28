package a2a

import (
	"context"
	"fmt"
	"time"

	a2apb "github.com/FluxGraph/fluxgraph/api/proto"
	"github.com/FluxGraph/fluxgraph/core"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type GRPCServer struct {
	a2apb.UnimplementedAgentServiceServer
	httpServer *Server // Reuse existing logic from REST server
}

func NewGRPCServer(httpServer *Server) *GRPCServer {
	return &GRPCServer{
		httpServer: httpServer,
	}
}

func (s *GRPCServer) SendMessage(ctx context.Context, req *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	tenantID, _ := ctx.Value("tenantID").(string)
	if tenantID == "" {
		tenantID = "default"
	}

	taskID := req.TaskId
	if taskID == "" {
		taskID = uuid.New().String()
	}

	// Map PB message to Core message
	coreMsg := mapPBToCoreMessage(req.Message)

	// Prepare AgentState
	state := core.NewState()
	state.TaskID = taskID
	state.ContextID = req.ContextId
	state = state.WithMessage(coreMsg)

	// Start Engine (reuse http server's engine)
	go func() {
		_, _ = s.httpServer.engine.Start(context.Background(), taskID, state)
	}()

	// Retrieve the task (or simulated task for response)
	task := &core.Task{
		ID:        taskID,
		ContextID: req.ContextId,
		TenantID:  tenantID,
		Status: core.TaskStatus{
			State:     core.TaskStateSubmitted,
			Timestamp: time.Now(),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return &a2apb.SendMessageResponse{
		Task: mapCoreToPBTask(task),
	}, nil
}

func (s *GRPCServer) GetTask(ctx context.Context, req *a2apb.GetTaskRequest) (*a2apb.Task, error) {
	task, err := s.httpServer.taskStore.GetByID(ctx, req.TaskId)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	return mapCoreToPBTask(task), nil
}

func (s *GRPCServer) StreamTaskEvents(req *a2apb.StreamTaskEventsRequest, stream a2apb.AgentService_StreamTaskEventsServer) error {
	// Implementation would involve subscribing to EventBus and sending to stream
	// This mirrors handleTaskEvents in server.go
	return nil // To be implemented in full later
}

// ---- Mappers ----

func mapPBToCoreMessage(pbMsg *a2apb.Message) core.Message {
	msg := core.Message{
		Role: core.Role(pbMsg.Role),
	}
	for _, p := range pbMsg.Parts {
		part := core.Part{}
		if t := p.GetText(); t != "" {
			part.Type = core.PartTypeText
			part.Text = t
		}
		// Tool calls/results mapping simplified for now
		msg.Parts = append(msg.Parts, part)
	}
	return msg
}

func mapCoreToPBTask(task *core.Task) *a2apb.Task {
	return &a2apb.Task{
		Id:        task.ID,
		ContextId: task.ContextID,
		TenantId:  task.TenantID,
		Status: &a2apb.TaskStatus{
			State:     string(task.Status.State),
			Detail:    task.Status.Message,
			Timestamp: timestamppb.New(task.Status.Timestamp),
		},
		CreatedAt: timestamppb.New(task.CreatedAt),
		UpdatedAt: timestamppb.New(task.UpdatedAt),
	}
}
