package scripts

import (
	"os/exec"
	"testing"
)

func TestPluginReleasePublisher(t *testing.T) {
	command := exec.Command("node", "--test", "plugin-release-publish.test.mjs")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin publisher tests: %v\n%s", err, output)
	}
}
