package orgpackage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPackageBuildsVersionString(t *testing.T) {
	runner := &fakeSFRunner{out: map[string]string{
		"/services/data/v65.0/tooling/query/?q=SELECT+Id%2C+SubscriberPackageId%2C+SubscriberPackage.Name%2C+SubscriberPackage.NamespacePrefix%2C+SubscriberPackageVersionId%2C+SubscriberPackageVersion.MajorVersion%2C+SubscriberPackageVersion.MinorVersion%2C+SubscriberPackageVersion.PatchVersion%2C+SubscriberPackageVersion.BuildNumber+FROM+InstalledSubscriberPackage+WHERE+SubscriberPackage.NamespacePrefix+%3D+%27pkg%27": readTestdata(t, "installed-package.json"),
	}}
	client := Client{Runner: runner, TargetOrg: "packaging", APIVersion: "65.0"}
	got, err := DiscoverPackage(context.Background(), client, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if got.Namespace != "pkg" || got.Name != "Billing Core" || got.Version != "1.2.3.4" {
		t.Fatalf("package = %#v", got)
	}
}

func TestCaptureApexClassesFromSymbolTable(t *testing.T) {
	runner := &fakeSFRunner{out: map[string]string{
		"/services/data/v65.0/tooling/query/?q=SELECT+Id%2C+Name%2C+NamespacePrefix%2C+SymbolTable%2C+ManageableState+FROM+ApexClass+WHERE+NamespacePrefix+%3D+%27pkg%27+AND+Status+%3D+%27Active%27": readTestdata(t, "tooling-apex-class-symboltable.json"),
	}}
	client := Client{Runner: runner, TargetOrg: "packaging", APIVersion: "65.0"}
	got, err := CaptureApexClasses(context.Background(), client, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "BillingGateway" {
		t.Fatalf("classes = %#v", got)
	}
	if len(got[0].Methods) != 1 || got[0].Methods[0].Parameters[0].Type != "Decimal" {
		t.Fatalf("methods = %#v", got[0].Methods)
	}
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
