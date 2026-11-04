//go:build e2e

package e2e

import (
	"os"
	"testing"
)

// The tests clone from localhost, which the panel refuses unless the box's owner opts in; child processes inherit this.
func TestMain(m *testing.M) {
	os.Setenv("SKIFF_ALLOW_PRIVATE_ENDPOINTS", "1")
	os.Exit(m.Run())
}
