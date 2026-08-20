package surfaceledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

var cacheCloseoutExceptionTypes = []string{
	"BulkApiKeysLimitExceededException",
	"CacheBuilderExecutionException",
	"CacheException",
	"ExecutionException",
	"InvalidCacheBuilderException",
	"InvalidParamException",
	"ItemSizeLimitExceededException",
	"OrgCacheException",
	"PlatformCacheInvalidOperationException",
	"SessionCacheException",
	"SessionCacheNoSessionException",
	"UnsupportedOperationException",
}

func cacheCloseoutOwners() map[string]string {
	const exceptionOwner = "core-runtime-cache-exception-typename-evidence"
	want := make(map[string]string, 60)
	special := map[string]bool{
		"OrgCacheException":              true,
		"SessionCacheException":          true,
		"SessionCacheNoSessionException": true,
	}
	for _, typeName := range cacheCloseoutExceptionTypes {
		prefix := "apex:Cache." + typeName
		want[prefix] = exceptionOwner
		want[fmt.Sprintf("%s.%s()", prefix, typeName)] = exceptionOwner
		want[fmt.Sprintf("%s.%s(Exception)", prefix, typeName)] = exceptionOwner
		want[fmt.Sprintf("%s.%s(String,Exception)", prefix, typeName)] = exceptionOwner
		if special[typeName] {
			want[fmt.Sprintf("%s.%s(String)", prefix, typeName)] = exceptionOwner
			want[prefix+".clone()"] = exceptionOwner
		}
	}
	for _, id := range []string{
		"apex:Cache.Visibility.NAMESPACE",
		"apex:Cache.Visibility.equals(Object)",
		"apex:Cache.Visibility.hashCode()",
		"apex:Cache.Visibility.ordinal()",
		"apex:Cache.Visibility.valueOf(String)",
		"apex:Cache.Visibility.values()",
	} {
		want[id] = "core-runtime-cache-partition-evidence"
	}
	return want
}

func TestCacheCloseoutHasExactExecutableOwnership(t *testing.T) {
	root := filepath.Join("..", "..")
	want := cacheCloseoutOwners()
	paths, err := filepath.Glob(filepath.Join(root, "docs", "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := BuildEvidenceSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	var selected []SurfaceLedgerRow
	for _, row := range evidence {
		if _, ok := want[row.SurfaceID]; ok {
			selected = append(selected, row)
		}
	}
	wantIDs := make([]string, 0, len(want))
	for id := range want {
		wantIDs = append(wantIDs, id)
	}
	assertExactSurfaceSet(t, selected, wantIDs)
	if len(wantIDs) != 60 {
		t.Fatalf("Cache closeout rows = %d, want 60", len(wantIDs))
	}
	for _, row := range selected {
		owner := "fixture:" + want[row.SurfaceID]
		if row.Evidence != EvidenceFixture || row.GladeBehavior != BehaviorSupported || len(row.Sources) != 1 || row.Sources[0] != owner {
			t.Fatalf("%s evidence/behavior/source = %s/%s/%v", row.SurfaceID, row.Evidence, row.GladeBehavior, row.Sources)
		}
	}

	fixtures := []string{
		"core-runtime-cache-exception-typename-evidence",
		"core-runtime-cache-partition-evidence",
	}
	sources := make(map[string]string, len(fixtures))
	for _, name := range fixtures {
		path := filepath.Join(root, "docs", "fixtures", name+".json")
		fixture, err := compat.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := compat.Validate(fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Command.Kind != "test" || len(fixture.Source) == 0 {
			t.Fatalf("fixture %s envelope = %#v", name, fixture)
		}
		var source strings.Builder
		for _, file := range fixture.Source {
			source.WriteString(file.Content)
		}
		sources[name] = source.String()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var policy struct {
			SalesforceEligible        *bool  `json:"salesforceEligible"`
			SalesforceExclusionClass  string `json:"salesforceExclusionClass"`
			SalesforceExclusionReason string `json:"salesforceExclusionReason"`
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			t.Fatal(err)
		}
		reason := strings.ToLower(policy.SalesforceExclusionReason)
		if policy.SalesforceEligible == nil || *policy.SalesforceEligible || policy.SalesforceExclusionClass != "policy-local-only" || (!strings.Contains(reason, "no hosted") && !strings.Contains(reason, "not a salesforce oracle")) {
			t.Fatalf("fixture %s local-only policy = %#v", name, policy)
		}
		if result, err := compat.Run(fixture); err != nil || !result.OK {
			t.Fatalf("fixture %s execution = %#v, error = %v", name, result, err)
		}
	}

	exceptionSource := sources["core-runtime-cache-exception-typename-evidence"]
	for _, typeName := range cacheCloseoutExceptionTypes {
		for _, witness := range []string{
			"new Cache." + typeName + "()",
			"new Cache." + typeName + "(cause)",
			"new Cache." + typeName + "('paired', cause)",
		} {
			if !strings.Contains(exceptionSource, witness) {
				t.Fatalf("exception source missing %q", witness)
			}
		}
	}
	for _, witness := range []string{
		"Cache.OrgCacheException orgClone = (Cache.OrgCacheException)orgMessage.clone();",
		"Cache.SessionCacheException sessionClone = (Cache.SessionCacheException)sessionMessage.clone();",
		"Cache.SessionCacheNoSessionException noSessionClone = (Cache.SessionCacheNoSessionException)noSessionMessage.clone();",
	} {
		if !strings.Contains(exceptionSource, witness) {
			t.Fatalf("special exception source missing %q", witness)
		}
	}
	visibilitySource := sources["core-runtime-cache-partition-evidence"]
	for _, witness := range []string{
		"Cache.Visibility namespaceVisibility = Cache.Visibility.NAMESPACE;",
		"namespaceVisibility.equals(namespaceVisibility)",
		"namespaceVisibility.hashCode()",
		"namespaceVisibility.ordinal()",
		"Cache.Visibility.valueOf('NAMESPACE')",
		"Cache.Visibility.values()",
	} {
		if !strings.Contains(visibilitySource, witness) {
			t.Fatalf("Visibility source missing %q", witness)
		}
	}
}
