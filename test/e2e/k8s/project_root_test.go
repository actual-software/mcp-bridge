package k8se2e

import (
	"strings"
	"testing"
)

func TestProjectRoot(t *testing.T) {
	stack := &KubernetesStack{}

	root, err := stack.getProjectRoot()
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}

	// Should end with /mcp not /mcp/test/e2e/k8s
	if strings.HasSuffix(root, "/test/e2e/k8s") {
		t.Errorf("Project root is wrong: %s (should be /Users/poile/repos/mcp)", root)
	}

	if !strings.HasSuffix(root, "/mcp") {
		t.Errorf("Project root doesn't end with /mcp: %s", root)
	}

	t.Logf("Project root: %s", root)
}
