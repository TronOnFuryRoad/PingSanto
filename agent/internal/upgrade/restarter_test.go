package upgrade

import (
	"context"
	"testing"
)

func TestExecRestarterMissingBinary(t *testing.T) {
	r := &ExecRestarter{}
	if err := r.Restart(context.Background(), "", nil, nil); err == nil {
		t.Fatalf("expected error for empty binary path")
	}
}
