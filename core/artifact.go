package core

// Artifact acts as an end-product output isolated from ongoing message processing.
type Artifact struct {
	ID       string
	Name     string
	Parts    []Part
	Metadata map[string]any
}

// AppendPart appends a part, useful for streaming scenarios.
func (a *Artifact) AppendPart(p Part) {
	a.Parts = append(a.Parts, p)
}

// ReplaceParts overrides current parts cleanly.
func (a *Artifact) ReplaceParts(parts []Part) {
	a.Parts = parts
}
