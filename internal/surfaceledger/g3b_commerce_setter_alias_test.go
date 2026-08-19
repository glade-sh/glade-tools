package surfaceledger

import (
	"testing"

	"github.com/glade-sh/glade/tools/internal/apexdocs"
)

func TestG3BCommerceSetterSourceStemsKeepCanonicalOwnership(t *testing.T) {
	rows := RowsFromDocsInventory(apexdocs.Inventory{Documents: []apexdocs.Document{
		{
			SourcePath: "apex/apex_commercepay_PostAuthResp_setAuthExpirationDate.md",
			Kind:       "document",
			Name:       "setAuthorizationExpirationDate(authExpDate)",
			Title:      "setAuthorizationExpirationDate(authExpDate)",
		},
		{
			SourcePath: "apex/apex_commercepay_PostAuthResp_setGatewayResultCodeDesc.md",
			Kind:       "document",
			Name:       "setGatewayResultCodeDescription(gatewayResultCodeDescription)",
			Title:      "setGatewayResultCodeDescription(gatewayResultCodeDescription)",
		},
	}})

	byID := rowsByID(rows)
	for _, id := range []string{
		ApexMemberID("commercepayments", "PostAuthorizationResponse", "setAuthorizationExpirationDate", []string{"authExpDate"}),
		ApexMemberID("commercepayments", "PostAuthorizationResponse", "setGatewayResultCodeDescription", []string{"gatewayResultCodeDescription"}),
	} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing canonical Commerce Payments setter row %s in %#v", id, rows)
		}
	}
	for _, row := range rows {
		if row.Namespace == "System" {
			t.Fatalf("Commerce Payments setter source emitted System-owned row %#v", row)
		}
	}
}
