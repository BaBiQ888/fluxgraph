package core

import (
	"errors"
)

type AgentStatus string

const (
	StatusRunning      AgentStatus = "Running"
	StatusPaused       AgentStatus = "Paused"
	StatusWaitingHuman AgentStatus = "WaitingHuman"
	StatusCompleted    AgentStatus = "Completed"
	StatusFailed       AgentStatus = "Failed"
)

var ErrVariableNotFound = errors.New("variable not found or type mismatch")

// AgentState holds the immutable snapshot of variables and context throughout the graph execution.
type AgentState struct {
	Messages     []Message
	Variables    map[string]any
	Artifacts    []Artifact
	
	StepCount    int
	RetryCount   int
	LastNodeID   string
	Status       AgentStatus
	
	CheckpointID string
	TaskID       string
	ContextID    string
}

// NewState produces a completely fresh and clean AgentState.
func NewState() *AgentState {
	return &AgentState{
		Messages:  []Message{},
		Variables: make(map[string]any),
		Artifacts: []Artifact{},
		Status:    StatusRunning,
	}
}

// WithMessage appends a message sequentially creating a fresh snapshot.
func (s *AgentState) WithMessage(m Message) *AgentState {
	ns := s.clone()
	ns.Messages = append(ns.Messages, m)
	return ns
}

// WithVariable merges or sets a key directly overriding older state traces safely.
func (s *AgentState) WithVariable(key string, val any) *AgentState {
	ns := s.clone()
	ns.Variables[key] = val
	return ns
}

// WithStatus transforms execution progression signals.
func (s *AgentState) WithStatus(status AgentStatus) *AgentState {
	ns := s.clone()
	ns.Status = status
	return ns
}

// LastMessage returns the most recent message or an empty message if none exist.
func (s *AgentState) LastMessage() Message {
	if len(s.Messages) == 0 {
		return Message{}
	}
	return s.Messages[len(s.Messages)-1]
}

// GetStringVariable exposes isolated casting logic.
func (s *AgentState) GetStringVariable(key string) (string, error) {
	v, ok := s.Variables[key]
	if !ok {
		return "", ErrVariableNotFound
	}
	str, ok := v.(string)
	if !ok {
		return "", ErrVariableNotFound
	}
	return str, nil
}

// clone performs deep isolation logic over maps and slices matching functional style logic requirements.
func (s *AgentState) clone() *AgentState {
	ns := &AgentState{
		StepCount:    s.StepCount,
		RetryCount:   s.RetryCount,
		LastNodeID:   s.LastNodeID,
		Status:       s.Status,
		CheckpointID: s.CheckpointID,
		TaskID:       s.TaskID,
		ContextID:    s.ContextID,
	}
	
	ns.Messages = make([]Message, len(s.Messages))
	copy(ns.Messages, s.Messages)
	
	ns.Variables = make(map[string]any, len(s.Variables))
	for k, v := range s.Variables {
		ns.Variables[k] = v
	}
	
	ns.Artifacts = make([]Artifact, len(s.Artifacts))
	copy(ns.Artifacts, s.Artifacts)
	
	return ns
}
