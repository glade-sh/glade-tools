package orgpackage

import (
	"context"
	"testing"

	"github.com/glade-sh/glade/internal/orgdescribe"
)

func TestCaptureObjectsIncludesNamespacedObjectsAndStandardFields(t *testing.T) {
	fieldDefinitionURL := (Client{APIVersion: "65.0"}).queryURL("tooling/query", "SELECT EntityDefinition.QualifiedApiName, QualifiedApiName FROM FieldDefinition WHERE NamespacePrefix = 'pkg'")
	runner := &fakeSFRunner{out: map[string]string{
		"/services/data/v65.0/sobjects": readTestdata(t, "describe-global.json"),
		fieldDefinitionURL:              `{"done":true,"records":[{"QualifiedApiName":"pkg__Billing_Profile__c","EntityDefinition":{"QualifiedApiName":"Account"}}]}`,
		"/services/data/v65.0/sobjects/pkg__Billing_Profile__c/describe": readTestdata(t, "describe-billing-profile.json"),
		"/services/data/v65.0/sobjects/Account/describe":                 readTestdata(t, "describe-account-with-package-field.json"),
	}}
	client := Client{Runner: runner, TargetOrg: "packaging", APIVersion: "65.0"}
	got, err := CaptureObjects(context.Background(), client, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "Account" || got[1].Name != "pkg__Billing_Profile__c" {
		t.Fatalf("objects = %#v", got)
	}
	s := (orgdescribe.Catalog{Objects: got}).ToSchema()
	if len(s.Objects) != 2 || s.Objects[0].Fields[0].Name != "pkg__Billing_Profile__c" {
		t.Fatalf("schema = %#v", s)
	}
	if len(got[0].Fields) != 1 || got[0].Fields[0].Name != "pkg__Billing_Profile__c" {
		t.Fatalf("standard object fields = %#v", got[0].Fields)
	}
	for _, call := range runner.calls {
		if call.URL == "/services/data/v65.0/sobjects/Contact/describe" {
			t.Fatalf("unexpected standard describe calls = %#v", runner.calls)
		}
	}
}

func TestCaptureObjectsFallsBackWhenFieldDefinitionUnsupported(t *testing.T) {
	fieldDefinitionURL := (Client{APIVersion: "65.0"}).queryURL("tooling/query", "SELECT EntityDefinition.QualifiedApiName, QualifiedApiName FROM FieldDefinition WHERE NamespacePrefix = 'pkg'")
	runner := &fakeSFRunner{out: map[string]string{
		"/services/data/v65.0/sobjects":                                  readTestdata(t, "describe-global.json"),
		"/services/data/v65.0/sobjects/pkg__Billing_Profile__c/describe": readTestdata(t, "describe-billing-profile.json"),
		"/services/data/v65.0/sobjects/Account/describe":                 readTestdata(t, "describe-account-with-package-field.json"),
		"/services/data/v65.0/sobjects/Contact/describe":                 readTestdata(t, "describe-contact.json"),
	}, errs: map[string]error{
		fieldDefinitionURL: errUnsupportedFieldDefinition,
	}}
	client := Client{Runner: runner, TargetOrg: "packaging", APIVersion: "65.0"}
	got, err := CaptureObjects(context.Background(), client, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Fields) != 1 || got[0].Fields[0].Name != "pkg__Billing_Profile__c" {
		t.Fatalf("objects = %#v", got)
	}
}

func TestCaptureMetadataInventory(t *testing.T) {
	runner := &fakeSFRunner{out: map[string]string{
		"/services/data/v65.0/tooling/query/?q=SELECT+Name%2C+NamespacePrefix+FROM+ExternalString+WHERE+NamespacePrefix+%3D+%27pkg%27":                                                `{"done":true,"records":[{"Name":"Billing_Error","NamespacePrefix":"pkg"}]}`,
		"/services/data/v65.0/tooling/query/?q=SELECT+Name%2C+NamespacePrefix+FROM+StaticResource+WHERE+NamespacePrefix+%3D+%27pkg%27":                                                `{"done":true,"records":[{"Name":"BillingAssets","NamespacePrefix":"pkg"}]}`,
		"/services/data/v65.0/tooling/query/?q=SELECT+DeveloperName%2C+NamespacePrefix%2C+MasterLabel%2C+IsExposed+FROM+LightningComponentBundle+WHERE+NamespacePrefix+%3D+%27pkg%27": `{"done":true,"records":[{"DeveloperName":"billingConsole","NamespacePrefix":"pkg","IsExposed":true}]}`,
		"/services/data/v65.0/tooling/query/?q=SELECT+DeveloperName%2C+NamespacePrefix+FROM+AuraDefinitionBundle+WHERE+NamespacePrefix+%3D+%27pkg%27":                                 `{"done":true,"records":[{"DeveloperName":"billingAura","NamespacePrefix":"pkg"}]}`,
	}}
	client := Client{Runner: runner, TargetOrg: "packaging", APIVersion: "65.0"}
	labels, resources, warnings, err := CaptureMetadataNames(context.Background(), client, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(labels) != 1 || labels[0] != "pkg__Billing_Error" || len(resources) != 1 || resources[0] != "pkg__BillingAssets" {
		t.Fatalf("metadata = %#v %#v", labels, resources)
	}
	bundles, warnings, err := CaptureLightningBundles(context.Background(), client, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(bundles) != 2 || bundles[0].Name != "billingAura" || bundles[1].Name != "billingConsole" {
		t.Fatalf("bundles = %#v", bundles)
	}
}
