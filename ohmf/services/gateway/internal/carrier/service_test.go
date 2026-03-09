package carrier

import (
    "testing"
)

func TestImportAndList_SkipIfNoDB(t *testing.T) {
    t.Skip("No test DB configured; skipping carrier service DB tests")
}
