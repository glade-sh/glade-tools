package toolcli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCorpusAssuranceHelpListsSealedWorkflow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"corpus", "assurance", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run returned %d, stderr=%s", code, stderr.String())
	}
	for _, command := range []string{"prepare", "usage", "replay", "merge-replay", "local-proof", "oracle-plan", "exclusion-request", "authorize-exclusions"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help omits %q:\n%s", command, stdout.String())
		}
	}
}
