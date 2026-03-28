package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExecutionContext(t *testing.T) {
	t.Run("require TenantID", func(t *testing.T) {
		ctx, err := NewContext(context.Background(), "", time.Second)
		assert.ErrorIs(t, err, ErrMissingTenantID)
		assert.Nil(t, ctx)
	})

	t.Run("deadline triggers cancel", func(t *testing.T) {
		ctx, err := NewContext(context.Background(), "tenant-test", 10*time.Millisecond)
		assert.NoError(t, err)
		
		<-ctx.Done()
		assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	})

	t.Run("derive inherits tenant", func(t *testing.T) {
		parent, _ := NewContext(context.Background(), "tenant-test", time.Second)
		parent.TraceID = "trace-tx"
		
		child := parent.Derive(time.Second)
		assert.Equal(t, "tenant-test", child.TenantID)
		assert.Equal(t, "trace-tx", child.TraceID)
		
		// Unrelated timeout cancel
		child.Cancel()
		assert.ErrorIs(t, child.Err(), context.Canceled)
		assert.NoError(t, parent.Err()) // Parent should not be canceled
	})
}
