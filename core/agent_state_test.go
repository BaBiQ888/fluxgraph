package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentStateImmutability(t *testing.T) {
	state := NewState()
	
	state2 := state.WithVariable("key1", "value1")
	assert.Empty(t, state.Variables, "original state should be unmodified")
	assert.Equal(t, "value1", state2.Variables["key1"])
	
	state3 := state2.WithMessage(Message{ID: "m1"})
	assert.Len(t, state2.Messages, 0, "state2 should be unmodified")
	assert.Len(t, state3.Messages, 1)

	strVal, err := state3.GetStringVariable("key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", strVal)
	
	// Error scenarios
	_, err = state3.GetStringVariable("invalid_key")
	assert.ErrorIs(t, err, ErrVariableNotFound)
	
	state4 := state3.WithVariable("key2", 123)
	_, err = state4.GetStringVariable("key2")
	assert.ErrorIs(t, err, ErrVariableNotFound)
}
