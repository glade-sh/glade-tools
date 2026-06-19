package orgpackage

import (
	"testing"
	"time"

	"github.com/glade-sh/glade/internal/orgdescribe"
)

func TestConvertCaptureToArtifact(t *testing.T) {
	capture := Capture{
		Package: PackageIdentity{
			Namespace:   "pkg",
			Name:        "Billing Core",
			Version:     "1.2.3.4",
			PackageID:   "033xx0000000001",
			InstalledID: "0A3xx0000000001",
		},
		Org: OrgIdentity{
			OrgID:      "00Dxx0000000001",
			Username:   "builder@example.com",
			TargetOrg:  "packaging",
			APIVersion: "65.0",
		},
		ApexClasses: []ApexClassContract{{
			Name:       "BillingGateway",
			Namespace:  "pkg",
			Visibility: "global",
			Methods: []ApexMethodContract{{
				Name:       "authorize",
				ReturnType: "Boolean",
				Visibility: "global",
				Static:     true,
				Parameters: []ApexParameterContract{{Name: "amount", Type: "Decimal"}},
			}},
		}},
		Objects: []orgdescribe.SObject{{
			Name: "pkg__Billing_Profile__c",
			Fields: []orgdescribe.Field{{
				Name:     "pkg__External_Key__c",
				Type:     "string",
				Nillable: true,
			}},
		}},
		Labels:          []string{"pkg__Billing_Error"},
		StaticResources: []string{"pkg__BillingAssets"},
		LightningBundles: []LightningBundleContract{{
			Namespace: "pkg",
			Name:      "billingConsole",
			Type:      "lwc",
			Exposed:   true,
		}},
		CapturedAt: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC),
	}
	artifact, err := Convert(capture)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Namespace != "pkg" || artifact.PackageName != "Billing Core" || artifact.Version != "1.2.3.4" {
		t.Fatalf("artifact identity = %#v", artifact)
	}
	if len(artifact.ApexTypes) != 1 || artifact.ApexTypes[0].Members[0].Name != "authorize" {
		t.Fatalf("apex types = %#v", artifact.ApexTypes)
	}
	if artifact.Labels != 1 || artifact.StaticResources != 1 {
		t.Fatalf("metadata counts = labels %d static %d", artifact.Labels, artifact.StaticResources)
	}
	if artifact.SourceHash == "" {
		t.Fatal("sourceHash is empty")
	}
}
