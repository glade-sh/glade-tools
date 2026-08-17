package corpusassurance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateReviewIndexDeduplicatesEvidenceAndBindsAttempt(t *testing.T) {
	root := t.TempDir()
	attemptPath := writeReviewIndexAttempt(t, root, "attempt")
	first := filepath.Join(root, "first.log")
	second := filepath.Join(root, "nested", "second.log")
	different := filepath.Join(root, "different.log")
	writeReviewIndexFile(t, first, "same evidence\n")
	writeReviewIndexFile(t, second, "same evidence\n")
	writeReviewIndexFile(t, different, "different evidence\n")
	output := filepath.Join(root, "REVIEW_INDEX.json")

	index, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{different, second, first}, OutputPath: output})
	if err != nil {
		t.Fatalf("CreateReviewIndex: %v", err)
	}
	if len(index.Artifacts) != 3 || len(index.Objects) != 2 {
		t.Fatalf("index counts = artifacts %d, objects %d", len(index.Artifacts), len(index.Objects))
	}
	if index.AttemptSHA256 != reviewIndexFileSHA256(t, attemptPath) {
		t.Fatalf("attempt hash = %q", index.AttemptSHA256)
	}
	if index.AttemptPath != attemptPath {
		t.Fatalf("attempt path = %q", index.AttemptPath)
	}
	if index.Artifacts[0].Path != different || index.Artifacts[1].Path != first || index.Artifacts[2].Path != second {
		t.Fatalf("artifacts were not sorted by exact path: %#v", index.Artifacts)
	}
	if index.Artifacts[1].SHA256 != index.Artifacts[2].SHA256 {
		t.Fatalf("duplicate evidence hashes differ: %#v", index.Artifacts)
	}
	objectCounts := map[int]bool{}
	for _, object := range index.Objects {
		objectCounts[object.ArtifactCount] = true
	}
	if !objectCounts[1] || !objectCounts[2] {
		t.Fatalf("object counts = %#v", index.Objects)
	}
	if loaded, err := LoadReviewIndex(output); err != nil || loaded.IndexSHA256 != index.IndexSHA256 {
		t.Fatalf("LoadReviewIndex = %#v, %v", loaded, err)
	}
	if _, err := VerifyReviewIndex(output); err != nil {
		t.Fatalf("VerifyReviewIndex: %v", err)
	}
}

func TestCreateReviewIndexBindsSuccessorToPriorIndexWithoutCopyingEvidence(t *testing.T) {
	root := t.TempDir()
	attemptPath := writeReviewIndexAttempt(t, root, "attempt")
	evidence := filepath.Join(root, "evidence.log")
	writeReviewIndexFile(t, evidence, "retained\n")
	priorPath := filepath.Join(root, "prior", "REVIEW_INDEX.json")
	if err := os.MkdirAll(filepath.Dir(priorPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{evidence}, OutputPath: priorPath}); err != nil {
		t.Fatalf("create prior index: %v", err)
	}

	successorAttempt := writeReviewIndexAttempt(t, root, "successor-attempt")
	successorPath := filepath.Join(root, "successor", "REVIEW_INDEX.json")
	if err := os.MkdirAll(filepath.Dir(successorPath), 0o700); err != nil {
		t.Fatal(err)
	}
	successor, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: successorAttempt, PredecessorPath: priorPath, ArtifactPaths: []string{evidence}, OutputPath: successorPath})
	if err != nil {
		t.Fatalf("create successor index: %v", err)
	}
	if successor.PredecessorSHA256 != reviewIndexFileSHA256(t, priorPath) {
		t.Fatalf("predecessor hash = %q", successor.PredecessorSHA256)
	}
	if successor.PredecessorPath != priorPath {
		t.Fatalf("predecessor path = %q", successor.PredecessorPath)
	}
	if len(successor.Objects) != 1 || successor.Artifacts[0].SHA256 != priorReviewIndexArtifactHash(t, priorPath) {
		t.Fatalf("successor did not reuse exact evidence identity: %#v", successor)
	}
	if _, err := os.Stat(filepath.Join(root, "successor", "evidence.log")); !os.IsNotExist(err) {
		t.Fatalf("successor copied evidence instead of indexing it: err=%v", err)
	}
	if _, err := VerifyReviewIndex(successorPath); err != nil {
		t.Fatalf("VerifyReviewIndex successor: %v", err)
	}
	writeReviewIndexFile(t, evidence, "mutated after successor\n")
	if _, err := VerifyReviewIndex(successorPath); err == nil {
		t.Fatal("VerifyReviewIndex accepted mutated predecessor evidence")
	}
}

func TestCreateReviewIndexRejectsInvalidPredecessor(t *testing.T) {
	root := t.TempDir()
	attemptPath := writeReviewIndexAttempt(t, root, "attempt")
	evidence := filepath.Join(root, "evidence.log")
	writeReviewIndexFile(t, evidence, "retained\n")
	priorPath := filepath.Join(root, "prior", "REVIEW_INDEX.json")
	if err := os.MkdirAll(filepath.Dir(priorPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{evidence}, OutputPath: priorPath}); err != nil {
		t.Fatalf("create prior index: %v", err)
	}
	writeReviewIndexFile(t, evidence, "changed before successor\n")
	successorPath := filepath.Join(root, "successor", "REVIEW_INDEX.json")
	if err := os.MkdirAll(filepath.Dir(successorPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: attemptPath, PredecessorPath: priorPath, ArtifactPaths: []string{evidence}, OutputPath: successorPath}); err == nil {
		t.Fatal("CreateReviewIndex accepted an invalid predecessor")
	}
}

func TestCreateReviewIndexRejectsUncleanAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	attemptPath := writeReviewIndexAttempt(t, root, "attempt")
	evidence := filepath.Join(root, "evidence.log")
	writeReviewIndexFile(t, evidence, "evidence\n")
	cleanOutput := filepath.Join(root, "REVIEW_INDEX.json")
	uncleanPath := func(path string) string {
		nested := filepath.Join(filepath.Dir(path), "nested")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		return nested + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(path)
	}
	priorPath := filepath.Join(root, "prior", "REVIEW_INDEX.json")
	if err := os.MkdirAll(filepath.Dir(priorPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{evidence}, OutputPath: priorPath}); err != nil {
		t.Fatalf("create valid predecessor: %v", err)
	}
	tests := []struct {
		name    string
		request ReviewIndexRequest
	}{
		{name: "attempt", request: ReviewIndexRequest{AttemptPath: uncleanPath(attemptPath), ArtifactPaths: []string{evidence}, OutputPath: cleanOutput}},
		{name: "output", request: ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{evidence}, OutputPath: uncleanPath(cleanOutput)}},
		{name: "artifact", request: ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{uncleanPath(evidence)}, OutputPath: cleanOutput}},
		{name: "predecessor", request: ReviewIndexRequest{AttemptPath: attemptPath, PredecessorPath: uncleanPath(priorPath), ArtifactPaths: []string{evidence}, OutputPath: cleanOutput}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CreateReviewIndex(test.request); err == nil || !strings.Contains(err.Error(), "path must be clean") {
				t.Fatalf("CreateReviewIndex unclean path error = %v", err)
			}
		})
	}
	uncleanIndex := uncleanPath(priorPath)
	if _, err := LoadReviewIndex(uncleanIndex); err == nil || !strings.Contains(err.Error(), "path must be clean") {
		t.Fatalf("LoadReviewIndex unclean path error = %v", err)
	}
	if _, err := VerifyReviewIndex(uncleanIndex); err == nil || !strings.Contains(err.Error(), "path must be clean") {
		t.Fatalf("VerifyReviewIndex unclean path error = %v", err)
	}
}

func TestVerifyReviewIndexRejectsPredecessorCycle(t *testing.T) {
	root := t.TempDir()
	attemptPath := writeReviewIndexAttempt(t, root, "attempt")
	evidence := filepath.Join(root, "evidence.log")
	writeReviewIndexFile(t, evidence, "cycle evidence\n")
	evidenceData, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	artifact := ReviewIndexArtifact{Path: evidence, SHA256: replayBytesSHA256(evidenceData), Size: int64(len(evidenceData))}
	object := ReviewIndexObject{SHA256: artifact.SHA256, Size: artifact.Size, ArtifactCount: 1}
	pathA := filepath.Join(root, "A", "REVIEW_INDEX.json")
	pathB := filepath.Join(root, "B", "REVIEW_INDEX.json")
	base := func(predecessorPath, predecessorSHA string) ReviewIndex {
		index := ReviewIndex{SchemaVersion: 1, AttemptPath: attemptPath, AttemptSHA256: reviewIndexFileSHA256(t, attemptPath), PredecessorPath: predecessorPath, PredecessorSHA256: predecessorSHA, Artifacts: []ReviewIndexArtifact{artifact}, Objects: []ReviewIndexObject{object}}
		index.IndexSHA256 = reviewIndexSHA256(index)
		return index
	}
	if err := os.MkdirAll(filepath.Dir(pathA), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pathB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(pathA, base(pathB, strings.Repeat("a", 64))); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(pathB, base(pathA, strings.Repeat("b", 64))); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReviewIndex(pathA); err == nil || !strings.Contains(err.Error(), "predecessor cycle") {
		t.Fatalf("VerifyReviewIndex cycle error = %v", err)
	}
}

func TestReviewIndexRejectsArtifactMutationAndCreateOnlyOutput(t *testing.T) {
	root := t.TempDir()
	attemptPath := writeReviewIndexAttempt(t, root, "attempt")
	evidence := filepath.Join(root, "evidence.log")
	writeReviewIndexFile(t, evidence, "before\n")
	output := filepath.Join(root, "REVIEW_INDEX.json")
	if _, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{evidence}, OutputPath: output}); err != nil {
		t.Fatal(err)
	}
	writeReviewIndexFile(t, evidence, "after\n")
	if _, err := VerifyReviewIndex(output); err == nil {
		t.Fatal("VerifyReviewIndex accepted mutated evidence")
	}
	if _, err := CreateReviewIndex(ReviewIndexRequest{AttemptPath: attemptPath, ArtifactPaths: []string{evidence}, OutputPath: output}); err == nil {
		t.Fatal("CreateReviewIndex overwrote an existing index")
	}
}

func writeReviewIndexAttempt(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name, "ATTEMPT.json")
	attempt := AssuranceAttempt{
		SchemaVersion:            1,
		InventorySHA256:          strings.Repeat("a", 64),
		CandidateAuthoritySHA256: strings.Repeat("b", 64),
		Candidate:                RuntimeArtifact{Commit: strings.Repeat("c", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("d", 64)},
		Tools:                    RuntimeArtifact{Commit: strings.Repeat("e", 40), OS: "darwin", Arch: "arm64", SHA256: strings.Repeat("f", 64)},
		RemoteCleanupAuthoritySHA256: map[string]string{
			"replay-worker":     strings.Repeat("0", 64),
			"salesforce-worker": strings.Repeat("1", 64),
		},
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSON(path, attempt); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeReviewIndexFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func reviewIndexFileSHA256(t *testing.T, path string) string {
	t.Helper()
	hash, err := sha256FileDirect(path)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func priorReviewIndexArtifactHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var index ReviewIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	return index.Artifacts[0].SHA256
}
