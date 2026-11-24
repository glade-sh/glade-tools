package scripts

import (
	"os/exec"
	"testing"
)

func runHistoryPython(t *testing.T, names ...string) {
	t.Helper()
	args := append([]string{"-m", "unittest", "-v"}, names...)
	cmd := exec.Command("python3", args...)
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRoundTripRemote(t *testing.T) {
	runHistoryPython(t, "corpus_manager.tests.test_server.HTTPTests.test_export_reimport_preserves_all_run_statuses")
}

func TestHistoryNeighborsRemote(t *testing.T) {
	runHistoryPython(t, "corpus_manager.tests.test_history", "corpus_manager.tests.test_server")
}
