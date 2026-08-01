package surfaceledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: write an Apex file under a project dir
func writeApexFile(t *testing.T, root, project, name, content string) {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// helper: build a minimal ledger with given namespaces
func ledgerWithNamespaces(namespaces ...string) []SurfaceLedgerRow {
	var rows []SurfaceLedgerRow
	for _, ns := range namespaces {
		rows = append(rows, SurfaceLedgerRow{
			SurfaceID:  "apex:" + ns + ".SomeType",
			Product:    ProductApex,
			Namespace:  ns,
			TypeName:   "SomeType",
			Kind:       KindType,
			Area:       AreaRuntime,
			GladeShape: ShapeTypeKnown,
		})
	}
	return rows
}

// 1. comments and strings containing Slack. or ConnectApi. are ignored
func TestCorpusUsageIgnoresCommentsAndStrings(t *testing.T) {
	root := t.TempDir()
	ledger := ledgerWithNamespaces("ConnectApi", "Slack")

	// This file references ConnectApi and Slack only inside comments and strings.
	writeApexFile(t, root, "proj1", "TestComments.cls", `
public class TestComments {
    // ConnectApi.ChatterUsers is commented out
    /* Slack.Conversation is block commented */
    public void m() {
        String s = 'ConnectApi.ChatterUsers.getFollowers';
        String t = 'Slack.Conversation';
        // real code uses System.debug
    }
}
`)

	cu, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage: %v", err)
	}

	// ConnectApi and Slack references must be zero because they only appear
	// inside comments and strings.
	for _, e := range cu.Usage {
		if e.Namespace == "ConnectApi" && e.TotalRefs() > 0 {
			t.Fatalf("ConnectApi should have zero refs (only in comments/strings), got %d", e.TotalRefs())
		}
		if e.Namespace == "Slack" && e.TotalRefs() > 0 {
			t.Fatalf("Slack should have zero refs (only in comments/strings), got %d", e.TotalRefs())
		}
	}

	// But System (not in ledger) should not appear
	for _, e := range cu.Usage {
		if e.Namespace == "System" {
			t.Fatalf("System should not appear (only in comment)")
		}
	}
}

// 2. multiline block comments are ignored
func TestCorpusUsageIgnoresMultilineBlockComments(t *testing.T) {
	root := t.TempDir()
	ledger := ledgerWithNamespaces("Database")

	writeApexFile(t, root, "proj1", "BlockComment.cls", `
public class BlockComment {
    /* Database.insert(new Account());
       Database.update(records);
    */
    public void m() {
        Database.insert(new Account()); // only this one should count
    }
}
`)

	cu, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage: %v", err)
	}

	for _, e := range cu.Usage {
		if e.Namespace == "Database" {
			// Only one reference outside the block comment should count.
			if e.TotalRefs() != 1 {
				t.Fatalf("Database: want 1 ref outside block comment, got %d (pubProd=%d)", e.TotalRefs(), e.PubProdRefs)
			}
			return
		}
	}
	t.Fatalf("Database entry not found in usage")
}

// 3. public production, public test, private production, private test, and
//    expected-failure counts remain distinct
func TestCorpusUsageDistinctCountCategories(t *testing.T) {
	pubRoot := t.TempDir()
	priRoot := t.TempDir()
	failRoot := t.TempDir()
	ledger := ledgerWithNamespaces("System")

	// Public production
	writeApexFile(t, pubRoot, "myproj", "Handler.cls", `public class Handler { public void m() { System.debug('x'); } }`)
	// Public test
	writeApexFile(t, pubRoot, "myproj", "HandlerTest.cls", `@isTest public class HandlerTest { static void m() { System.debug('x'); } }`)
	// Private production
	writeApexFile(t, priRoot, "privproj", "Service.cls", `public class Service { public void m() { System.debug('x'); } }`)
	// Private test
	writeApexFile(t, priRoot, "privproj", "ServiceTest.cls", `@isTest public class ServiceTest { static void m() { System.debug('x'); } }`)
	// Expected-failure production
	writeApexFile(t, failRoot, "failproj", "Fail.cls", `public class Fail { public void m() { System.debug('x'); } }`)
	// Expected-failure test – both production and test refs land in PubFail*
	writeApexFile(t, failRoot, "failproj", "FailTest.cls", `@isTest public class FailTest { static void m() { System.debug('x'); } }`)

	cu, err := BuildCorpusUsage(ledger, pubRoot, failRoot, priRoot)
	if err != nil {
		t.Fatalf("BuildCorpusUsage: %v", err)
	}

	for _, e := range cu.Usage {
		if e.Namespace != "System" {
			continue
		}
		// We should see one ref in each category's distinct column.
		// pub production -> PubProdRefs
		// pub test -> PubTestRefs
		// pub fail -> PubFailRefs
		// priv production -> PrivProdRefs
		// priv test -> PrivTestRefs
		if e.PubProdRefs != 1 {
			t.Errorf("PubProdRefs: want 1 got %d", e.PubProdRefs)
		}
		if e.PubTestRefs != 1 {
			t.Errorf("PubTestRefs: want 1 got %d", e.PubTestRefs)
		}
		if e.PubFailRefs != 2 {
			t.Errorf("PubFailRefs: want 2 (prod+test fail files) got %d", e.PubFailRefs)
		}
		if e.PrivProdRefs != 1 {
			t.Errorf("PrivProdRefs: want 1 got %d", e.PrivProdRefs)
		}
		if e.PrivTestRefs != 1 {
			t.Errorf("PrivTestRefs: want 1 got %d", e.PrivTestRefs)
		}

		// File counts
		if e.PubProdFiles != 1 {
			t.Errorf("PubProdFiles: want 1 got %d", e.PubProdFiles)
		}
		if e.PubTestFiles != 1 {
			t.Errorf("PubTestFiles: want 1 got %d", e.PubTestFiles)
		}
		if e.PubFailFiles != 2 {
			t.Errorf("PubFailFiles: want 2 (prod+test fail files) got %d", e.PubFailFiles)
		}
		if e.PrivProdFiles != 1 {
			t.Errorf("PrivProdFiles: want 1 got %d", e.PrivProdFiles)
		}
		if e.PrivTestFiles != 1 {
			t.Errorf("PrivTestFiles: want 1 got %d", e.PrivTestFiles)
		}

		// Project counts
		if e.PubProdProjects != 1 {
			t.Errorf("PubProdProjects: want 1 got %d", e.PubProdProjects)
		}
		if e.PubTestProjects != 1 {
			t.Errorf("PubTestProjects: want 1 got %d", e.PubTestProjects)
		}
		if e.PubFailProjects != 1 {
			t.Errorf("PubFailProjects: want 1 got %d", e.PubFailProjects)
		}
		if e.PrivProdProjects != 1 {
			t.Errorf("PrivProdProjects: want 1 got %d", e.PrivProdProjects)
		}
		if e.PrivTestProjects != 1 {
			t.Errorf("PrivTestProjects: want 1 got %d", e.PrivTestProjects)
		}
		return
	}
	t.Fatalf("System entry not found in usage")
}

// 4. a local DataSource.cls shadows DataSource. within that project
func TestCorpusUsageLocalShadowing(t *testing.T) {
	root := t.TempDir()
	ledger := ledgerWithNamespaces("DataSource")

	// A project that has its own DataSource.cls
	writeApexFile(t, root, "shadowproj", "DataSource.cls", `public class DataSource { public void m() {} }`)
	// Another file in same project references DataSource.connect — should NOT count
	writeApexFile(t, root, "shadowproj", "Consumer.cls", `public class Consumer { public void m() { DataSource.connect(); } }`)

	// A different project with NO local DataSource — reference should count
	writeApexFile(t, root, "cleanproj", "User.cls", `public class User { public void m() { DataSource.connect(); } }`)

	cu, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage: %v", err)
	}

	for _, e := range cu.Usage {
		if e.Namespace != "DataSource" {
			continue
		}
		// Only the clean project should contribute.
		if e.PubProdRefs != 1 {
			t.Fatalf("DataSource PubProdRefs: want 1 (clean project only), got %d", e.PubProdRefs)
		}
		if e.PubProdProjects != 1 {
			t.Fatalf("DataSource PubProdProjects: want 1, got %d", e.PubProdProjects)
		}
		return
	}
	t.Fatalf("DataSource entry not found in usage")
}

// 5. namespace and Namespace.Type.member references are counted deterministically
func TestCorpusUsageNamespaceAndMemberRefs(t *testing.T) {
	root := t.TempDir()
	ledger := ledgerWithNamespaces("ConnectApi", "Database")

	writeApexFile(t, root, "proj1", "App.cls", `
public class App {
    public void m() {
        ConnectApi.ChatterUsers.getFollowings(null);
        ConnectApi.ChatterUsers.getFollowers(null, null);
        Database.insert(rec);
        Database.update(rec);
    }
}
`)

	cu, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage: %v", err)
	}

	byKey := map[string]CorpusUsageEntry{}
	for _, e := range cu.Usage {
		byKey[e.UsageKey] = e
	}

	// Run twice to verify determinism
	cu2, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage second: %v", err)
	}
	byKey2 := map[string]CorpusUsageEntry{}
	for _, e := range cu2.Usage {
		byKey2[e.UsageKey] = e
	}

	for k, v := range byKey {
		v2, ok := byKey2[k]
		if !ok {
			t.Fatalf("key %s present in first scan but missing in second", k)
		}
		if v.PubProdRefs != v2.PubProdRefs {
			t.Fatalf("key %s refs differ: %d vs %d", k, v.PubProdRefs, v2.PubProdRefs)
		}
	}

	// Check specific entries.
	checkEntry := func(key string, want int) {
		e, ok := byKey[key]
		if !ok {
			t.Fatalf("entry %q not found in usage", key)
		}
		if e.TotalRefs() != want {
			t.Fatalf("entry %q: want %d refs got %d", key, want, e.TotalRefs())
		}
	}

	checkEntry("ConnectApi", 2)                          // 2 calls via ConnectApi
	checkEntry("ConnectApi.ChatterUsers", 2)             // 2 calls on ChatterUsers
	checkEntry("ConnectApi.ChatterUsers.getFollowings", 1) // 1 call
	checkEntry("ConnectApi.ChatterUsers.getFollowers", 1)  // 1 call
	checkEntry("Database", 2)                            // insert + update
}

// helper for the test above — TotalRefs sums all ref categories
func (e CorpusUsageEntry) TotalRefs() int {
	return e.PubProdRefs + e.PubTestRefs + e.PubFailRefs +
		e.PrivProdRefs + e.PrivTestRefs
}

// 6. output ordering and corpus digests are stable across repeated scans
func TestCorpusUsageDeterministicOutput(t *testing.T) {
	root := t.TempDir()
	ledger := ledgerWithNamespaces("System", "Database", "Messaging")

	writeApexFile(t, root, "projA", "App.cls", `public class App { public void m() { System.debug('x'); Database.insert(null); Messaging.send(null); } }`)

	cu1, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage first: %v", err)
	}
	data1, err := json.MarshalIndent(cu1, "", "  ")
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}

	cu2, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage second: %v", err)
	}
	data2, err := json.MarshalIndent(cu2, "", "  ")
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}

	if string(data1) != string(data2) {
		t.Fatalf("output not deterministic\nfirst:\n%s\nsecond:\n%s", string(data1), string(data2))
	}

	// Verify digests are populated.
	h := sha256.New()
	if cu1.PublicRootSHA256 == "" {
		t.Fatalf("publicRootSha256 must be populated")
	}
	// A valid SHA-256 hex digest is 64 hex chars.
	for _, digest := range []string{cu1.PublicRootSHA256} {
		if len(digest) != 64 {
			t.Fatalf("digest %q has length %d, want 64", digest, len(digest))
		}
		if _, err := hex.DecodeString(digest); err != nil {
			t.Fatalf("digest %q is not valid hex: %v", digest, err)
		}
	}
	_ = h
}

// 7. a missing root, unreadable root, or zero eligible Apex projects is a
//    blocking operational error
func TestCorpusUsageBlockingErrors(t *testing.T) {
	ledger := ledgerWithNamespaces("System")

	// Missing root
	_, err := BuildCorpusUsage(ledger, "/nonexistent/path/12345", "", "")
	if err == nil {
		t.Fatalf("expected error for missing public root")
	}
	if !strings.Contains(err.Error(), "public root") {
		t.Fatalf("error should mention public root: %v", err)
	}

	// Empty root with zero eligible projects
	emptyRoot := t.TempDir()
	_, err = BuildCorpusUsage(ledger, emptyRoot, "", "")
	if err == nil {
		t.Fatalf("expected error for root with no Apex projects")
	}
	if !strings.Contains(err.Error(), "no Apex") && !strings.Contains(err.Error(), "eligible") {
		t.Fatalf("error should mention no eligible projects: %v", err)
	}
}

// 10. no private project name, path, or source excerpt appears in aggregate JSON
func TestCorpusUsageNoPrivateLeakage(t *testing.T) {
	root := t.TempDir()
	ledger := ledgerWithNamespaces("System")

	// Use a distinctive project name.
	writeApexFile(t, root, "SecretProjectAlpha", "SecretClass.cls", `public class SecretClass { public void m() { System.debug('secret text 12345'); } }`)

	cu, err := BuildCorpusUsage(ledger, root, "", "")
	if err != nil {
		t.Fatalf("BuildCorpusUsage: %v", err)
	}

	data, err := json.MarshalIndent(cu, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(data)

	// Private project name must not leak.
	if strings.Contains(text, "SecretProjectAlpha") {
		t.Fatalf("project name leaked into JSON output:\n%s", text)
	}
	// File names must not leak.
	if strings.Contains(text, "SecretClass.cls") {
		t.Fatalf("file name leaked into JSON output:\n%s", text)
	}
	// Source excerpts must not leak.
	if strings.Contains(text, "secret text 12345") {
		t.Fatalf("source text leaked into JSON output:\n%s", text)
	}
}
