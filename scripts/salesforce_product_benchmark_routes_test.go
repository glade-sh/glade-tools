package scripts

import (
	"os/exec"
	"testing"
)

func TestSalesforceProductBenchmarkDiscoveryRouteTwins(t *testing.T) {
	cmd := exec.Command("python3", "-m", "unittest", "scripts/test_salesforce_product_benchmark_routes.py")
	cmd.Dir = toolsRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("benchmark discovery routes: %v\n%s", err, out)
	}
}
