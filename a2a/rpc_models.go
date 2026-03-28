package a2a

import (
	"encoding/json"

	"github.com/FluxGraph/fluxgraph/core"
)

// RPCRequest represents a JSON-RPC 2.0 request.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

// RPCResponse represents a JSON-RPC 2.0 response.
type RPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
	ID      any    `json:"id"`
}

// RPCError represents a JSON-RPC 2.0 error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// A2A-specific JSON-RPC error codes.
const (
	RPCCodeParseError     = -32700
	RPCCodeInvalidRequest = -32600
	RPCCodeMethodNotFound = -32601
	RPCCodeInvalidParams  = -32602
	RPCCodeInternalError  = -32603

	// Business error codes (mapped from A2A spec)
	RPCCodeTaskNotFound   = -32001
	RPCCodeUnauthorized   = -32002
)

// SendMessageParams defines the parameters for "message/send".
type SendMessageParams struct {
	Message            core.Message   `json:"message"`
	TaskID             string         `json:"taskId,omitempty"`
	ContextID          string         `json:"contextId,omitempty"`
	ReturnImmediately  bool           `json:"returnImmediately,omitempty"`
	Configuration      map[string]any `json:"configuration,omitempty"`
}

type CreatePushConfigParams struct {
	TaskID      string             `json:"taskId"`
	URL         string             `json:"url"`
	Secret      string             `json:"secret,omitempty"`
	EventTypes  []string           `json:"eventTypes,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}
