package scripts

import (
	"os"
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
