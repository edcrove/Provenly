package auditnote

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNote(t *testing.T) {
	Project(context.Background(), 3) // without a note: nothing to record, no panic
	ctx, n := Open(context.Background())
	assert.Zero(t, n.ProjectID())
	Project(ctx, 2)
	Project(ctx, 5)
	assert.Equal(t, int64(2), n.ProjectID(), "the first project allowed wins")
}
