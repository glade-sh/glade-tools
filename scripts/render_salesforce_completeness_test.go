package scripts

import (
	"os/exec"
	"testing"
)

func TestRenderSalesforceCompleteness(t *testing.T) {
	command := exec.Command("bash", "render-salesforce-completeness.test.sh")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("render Salesforce completeness status: %v\n%s", err, output)
	}
}
