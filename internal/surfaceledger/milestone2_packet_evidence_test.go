package surfaceledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glade-sh/glade/tools/internal/compat"
)

const milestone2PacketFixture = "milestone2-api-defaults-packet-82a03812.json"

var milestone2PacketSurfaceIDs = []string{
	"unknown:milestone2-api66-body-caller66-database-default",
	"unknown:milestone2-api66-body-caller67-database-default",
	"unknown:milestone2-api67-body-caller66-database-default",
	"unknown:milestone2-api67-body-caller67-database-default",
	"unknown:milestone2-api66-sharing-default",
	"unknown:milestone2-api67-sharing-default",
	"unknown:milestone2-api67-trigger-database-user-mode",
	"unknown:milestone2-explicit-user-dml",
	"unknown:milestone2-explicit-system-dml",
	"unknown:milestone2-api66-multiline",
	"unknown:milestone2-api67-multiline",
	"unknown:milestone2-api66-live-opening-newline",
	"unknown:milestone2-api67-live-opening-newline",
	"unknown:milestone2-api66-live-closing-same-line",
	"unknown:milestone2-api67-live-closing-same-line",
	"unknown:milestone2-api66-live-quote-runs",
	"unknown:milestone2-api67-live-quote-runs",
	"unknown:milestone2-api66-live-backslash",
	"unknown:milestone2-api67-live-backslash",
	"unknown:milestone2-api66-live-indentation",
	"unknown:milestone2-api67-live-indentation",
	"unknown:milestone2-api66-live-empty",
	"unknown:milestone2-api67-live-empty",
	"unknown:milestone2-api66-live-syntax-negative",
	"unknown:milestone2-api67-live-syntax-negative",
	"unknown:milestone2-api66-live-crlf-normalization",
	"unknown:milestone2-api67-live-crlf-normalization",
	"unknown:milestone2-api67-metadata-crlf-nonparity",
}

func TestMilestone2PacketLedgerUsesExactVersionCases(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "fixtures", milestone2PacketFixture)
	fixture, err := compat.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := compat.Validate(fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Command.Kind != "policy-evidence" {
		t.Fatalf("command kind = %q, want policy-evidence", fixture.Command.Kind)
	}
	if len(fixture.Evidence) != len(milestone2PacketSurfaceIDs) {
		t.Fatalf("packet evidence rows = %d, want %d", len(fixture.Evidence), len(milestone2PacketSurfaceIDs))
	}
	want := make(map[string]bool, len(milestone2PacketSurfaceIDs))
	for _, id := range milestone2PacketSurfaceIDs {
		want[id] = true
	}
	for _, evidence := range fixture.Evidence {
		if !want[evidence.SurfaceID] {
			t.Fatalf("unexpected packet surface %q", evidence.SurfaceID)
		}
		if strings.ContainsAny(evidence.SurfaceID, "*?") || strings.Contains(strings.ToLower(evidence.Notes), "wildcard") {
			t.Fatalf("packet row %q uses wildcard credit", evidence.SurfaceID)
		}
		if !strings.Contains(evidence.Notes, "case=") {
			t.Fatalf("packet row %q lacks an exact case id", evidence.SurfaceID)
		}
		if !strings.Contains(evidence.Notes, "candidate=82a0381269b88e68d465e5c23fd08c08136e406f") {
			t.Fatalf("packet row %q is not bound to the reviewed candidate", evidence.SurfaceID)
		}
	}
	rows, err := BuildEvidenceSnapshot([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(milestone2PacketSurfaceIDs) {
		t.Fatalf("ledger rows = %d, want %d", len(rows), len(milestone2PacketSurfaceIDs))
	}
}

func TestMilestone2ReceiptBindsBothCandidatesAndRetainsRawArtifacts(t *testing.T) {
	toolsRoot := filepath.Join("..", "..")
	fixturePath := filepath.Join(toolsRoot, "docs", "fixtures", milestone2PacketFixture)
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Provenance struct {
			ProductCommit string `json:"productCommit"`
			ReceiptPath   string `json:"receiptPath"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(fixtureData, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Provenance.ProductCommit == "" || fixture.Provenance.ReceiptPath == "" {
		t.Fatal("milestone 2 fixture is missing receipt provenance")
	}
	evidenceRoot := filepath.Join(toolsRoot, "..", "..", "..", "glade-evidence")
	receiptPath := filepath.Join(evidenceRoot, filepath.FromSlash(fixture.Provenance.ReceiptPath))
	receiptData, err := os.ReadFile(receiptPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("milestone 2 raw receipt is not present at %s", receiptPath)
	}
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		SchemaVersion int    `json:"schemaVersion"`
		ProductCommit string `json:"productCommit"`
		ToolsCommit   string `json:"toolsCommit"`
		Commands      []struct {
			ID            string `json:"id"`
			Command       string `json:"command"`
			CWD           string `json:"cwd"`
			Status        string `json:"status"`
			BlockedReason string `json:"blockedReason"`
			ExitCode      int    `json:"exitCode"`
			StdoutPath    string `json:"stdoutPath"`
			StdoutSHA256  string `json:"stdoutSha256"`
			StderrPath    string `json:"stderrPath"`
			StderrSHA256  string `json:"stderrSha256"`
		} `json:"commands"`
		Artifacts []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.SchemaVersion != 1 || receipt.ProductCommit != fixture.Provenance.ProductCommit || !fullSHA(receipt.ProductCommit) || !fullSHA(receipt.ToolsCommit) {
		t.Fatalf("receipt candidate binding = schema:%d product:%q tools:%q", receipt.SchemaVersion, receipt.ProductCommit, receipt.ToolsCommit)
	}
	currentToolsCommit, err := exec.Command("git", "-C", toolsRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(currentToolsCommit)) != receipt.ToolsCommit {
		t.Fatalf("receipt tools commit = %s, current tools HEAD = %s", receipt.ToolsCommit, strings.TrimSpace(string(currentToolsCommit)))
	}
	if len(receipt.Commands) < 3 {
		t.Fatalf("receipt commands = %d, want raw focused, broad, and external commands", len(receipt.Commands))
	}
	seenCommand := map[string]bool{}
	for _, command := range receipt.Commands {
		if command.ID == "" || command.Command == "" || command.CWD == "" {
			t.Fatalf("invalid command receipt = %#v", command)
		}
		if command.ID == "salesforce-live" && command.Status == "blocked" {
			if command.ExitCode == 0 || command.BlockedReason == "" {
				t.Fatalf("blocked Salesforce command lacks an explicit boundary: %#v", command)
			}
		} else if command.Status != "pass" || command.ExitCode != 0 {
			t.Fatalf("non-passing required command = %#v", command)
		}
		seenCommand[command.ID] = true
		assertReceiptArtifact(t, evidenceRoot, command.StdoutPath, command.StdoutSHA256)
		assertReceiptArtifact(t, evidenceRoot, command.StderrPath, command.StderrSHA256)
	}
	for _, want := range []string{"product-focused", "product-broad", "salesforce-live"} {
		if !seenCommand[want] {
			t.Fatalf("receipt is missing command %q", want)
		}
	}
	seenKind := map[string]bool{}
	for _, artifact := range receipt.Artifacts {
		if artifact.ID == "" || artifact.Kind == "" {
			t.Fatalf("invalid retained artifact = %#v", artifact)
		}
		seenKind[artifact.Kind] = true
		assertReceiptArtifact(t, evidenceRoot, artifact.Path, artifact.SHA256)
	}
	for _, want := range []string{"raw-salesforce-input", "raw-salesforce-output"} {
		if !seenKind[want] {
			t.Fatalf("receipt is missing artifact kind %q", want)
		}
	}
}

func fullSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func assertReceiptArtifact(t *testing.T, root, relative, want string) {
	t.Helper()
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) == "." || strings.HasPrefix(filepath.Clean(relative), ".."+string(filepath.Separator)) || !fullSHA(want) {
		t.Fatalf("invalid receipt artifact path or hash: path=%q sha=%q", relative, want)
	}
	data, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if got := hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("receipt artifact %s SHA-256 = %s, want %s", relative, got, want)
	}
}
