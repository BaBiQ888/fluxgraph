package core

type PartType string

const (
	PartTypeText           PartType = "Text"
	PartTypeFile           PartType = "File"
	PartTypeStructuredData PartType = "StructuredData"
	PartTypeToolCall       PartType = "ToolCall"
	PartTypeToolResult     PartType = "ToolResult"
)

type FileRef struct {
	URI      string
	MIMEType string
}

type ToolCallPart struct {
	CallID    string
	ToolName  string
	Arguments map[string]any
}

type ToolResultPart struct {
	CallID  string
	Result  string
	IsError bool
}

// Part represents the smallest content unit of a message.
type Part struct {
	Type           PartType
	Text           string          `json:",omitempty"`
	File           *FileRef        `json:",omitempty"`
	StructuredData map[string]any  `json:",omitempty"`
	ToolCall       *ToolCallPart   `json:",omitempty"`
	ToolResult     *ToolResultPart `json:",omitempty"`
}
