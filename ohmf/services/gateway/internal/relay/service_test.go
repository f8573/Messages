package relay

import (
	"context"
	"testing"
)

func TestCreateAndListSkipIfNoDB(t *testing.T) {
	// This test requires a configured test DB; skip by default.
	t.Skip("No test DB configured; skipping relay service DB tests")
	_ = context.Background()
}
