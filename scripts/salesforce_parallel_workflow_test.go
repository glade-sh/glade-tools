package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSalesforceParallelWorkflowAggregateIsSoleAuthority(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(toolsRoot(t), ".github/workflows/salesforce-correctness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, marker := range []string{
		"  product-plan:", "  product-shards:", "fail-fast: false",
		"matrix: ${{ fromJSON(needs.product-plan.outputs.matrix) }}",
		"needs: [product-plan, product-shards]",
		"needs.product-plan.result == 'success' && needs.product-shards.result == 'success'",
		"scripts/salesforce-product-tests.sh prepare", "scripts/salesforce-product-tests.sh shard",
		"name: salesforce-product-${{ matrix.id }}", "pattern: salesforce-product-*", "merge-multiple: false",
		"SALESFORCE_PRODUCT_SHARDS:", "actions/download-artifact@",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("parallel workflow missing %q", marker)
		}
	}
	aggregate := strings.Index(text, "  salesforce-correctness:")
	publish := strings.Index(text, "      - name: Publish Salesforce release authority")
	if aggregate < 0 || publish < aggregate || strings.Count(text, "scripts/publish-salesforce-authority.sh") != 1 || strings.Count(text, "permission-checks: write") != 1 {
		t.Fatal("aggregate must be the sole release authority publisher")
	}
	if strings.Contains(text[:max(aggregate, 0)], "SF_SFDX_AUTH_URL") {
		t.Fatal("product workers must remain credential-free")
	}
}

func TestSalesforceProductParallelSyntheticRoutes(t *testing.T) {
	cmd := exec.Command("python3", "-m", "unittest", "scripts/test_salesforce_product_parallel.py")
	cmd.Dir = toolsRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("parallel product routes: %v\n%s", err, out)
	}
}

// GitHub rejects a workflow file whose job-level env uses the runner context
// (it is only available in steps), which shows up as a failed "workflow file
// issue" run before any job starts.
func TestSalesforceCorrectnessWorkflowJobEnvAvoidsRunnerContext(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "salesforce-correctness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	inJobEnv := false
	for number, line := range strings.Split(string(data), "\n") {
		switch {
		case line == "    env:":
			inJobEnv = true
		case inJobEnv && !strings.HasPrefix(line, "      ") && strings.TrimSpace(line) != "":
			inJobEnv = false
		}
		if inJobEnv && strings.Contains(line, "${{ runner.") {
			t.Errorf("line %d: job-level env must not use the runner context: %s", number+1, strings.TrimSpace(line))
		}
	}
}
