package corpusassurance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ReviewIndex is a compact, hash-bound view of retained evidence. It records
// every source path but stores identical content once in Objects. The index
// never copies or removes the raw evidence.
type ReviewIndex struct {
	SchemaVersion     int                   `json:"schemaVersion"`
	AttemptPath       string                `json:"attemptPath"`
	AttemptSHA256     string                `json:"attemptSha256"`
	PredecessorPath   string                `json:"predecessorPath,omitempty"`
	PredecessorSHA256 string                `json:"predecessorSha256,omitempty"`
	Artifacts         []ReviewIndexArtifact `json:"artifacts"`
	Objects           []ReviewIndexObject   `json:"objects"`
	IndexSHA256       string                `json:"indexSha256"`
}

type ReviewIndexArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type ReviewIndexObject struct {
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	ArtifactCount int    `json:"artifactCount"`
}

type ReviewIndexRequest struct {
	AttemptPath     string
	PredecessorPath string
	ArtifactPaths   []string
	OutputPath      string
}

type reviewIndexDigest struct {
	SchemaVersion     int                   `json:"schemaVersion"`
	AttemptPath       string                `json:"attemptPath"`
	AttemptSHA256     string                `json:"attemptSha256"`
	PredecessorPath   string                `json:"predecessorPath,omitempty"`
	PredecessorSHA256 string                `json:"predecessorSha256,omitempty"`
	Artifacts         []ReviewIndexArtifact `json:"artifacts"`
	Objects           []ReviewIndexObject   `json:"objects"`
}

// CreateReviewIndex reads each artifact once, then writes one create-only
// index. Sorting and object aggregation make the result deterministic across
// retries while preserving each failed attempt's original paths.
func CreateReviewIndex(request ReviewIndexRequest) (ReviewIndex, error) {
	for _, pathLabel := range []struct {
		path  string
		label string
	}{{request.AttemptPath, "attempt"}, {request.OutputPath, "review index output"}} {
		if err := validateCleanReviewPath(pathLabel.path, pathLabel.label); err != nil {
			return ReviewIndex{}, err
		}
	}
	if len(request.ArtifactPaths) == 0 {
		return ReviewIndex{}, fmt.Errorf("at least one review artifact is required")
	}
	if request.PredecessorPath != "" {
		if err := validateCleanReviewPath(request.PredecessorPath, "predecessor review index"); err != nil {
			return ReviewIndex{}, err
		}
	}
	if _, err := os.Lstat(request.OutputPath); err == nil {
		return ReviewIndex{}, fmt.Errorf("review index output already exists: %s", request.OutputPath)
	} else if !os.IsNotExist(err) {
		return ReviewIndex{}, err
	}

	attempt, attemptBytes, err := readReviewIndexAttempt(request.AttemptPath)
	if err != nil {
		return ReviewIndex{}, fmt.Errorf("read attempt: %w", err)
	}
	if err := ValidateAssuranceAttempt(attempt); err != nil {
		return ReviewIndex{}, fmt.Errorf("validate attempt: %w", err)
	}

	attemptPath := filepath.Clean(request.AttemptPath)
	predecessorPath := ""
	var predecessorSHA string
	if request.PredecessorPath != "" {
		predecessorPath = filepath.Clean(request.PredecessorPath)
		if _, predecessorBytes, err := verifyReviewIndex(predecessorPath, make(map[string]struct{})); err != nil {
			return ReviewIndex{}, fmt.Errorf("read predecessor review index: %w", err)
		} else {
			predecessorSHA = replayBytesSHA256(predecessorBytes)
		}
	}

	artifacts := make([]ReviewIndexArtifact, 0, len(request.ArtifactPaths))
	seenPaths := make(map[string]struct{}, len(request.ArtifactPaths))
	for _, path := range request.ArtifactPaths {
		if err := validateCleanReviewPath(path, "review artifact"); err != nil {
			return ReviewIndex{}, err
		}
		if path == filepath.Clean(request.OutputPath) {
			return ReviewIndex{}, fmt.Errorf("review index output cannot also be an artifact")
		}
		if _, exists := seenPaths[path]; exists {
			return ReviewIndex{}, fmt.Errorf("duplicate review artifact path: %s", path)
		}
		seenPaths[path] = struct{}{}
		snapshot, err := readRegularFileSnapshot(path)
		if err != nil {
			return ReviewIndex{}, fmt.Errorf("read review artifact %s: %w", path, err)
		}
		artifacts = append(artifacts, ReviewIndexArtifact{Path: path, SHA256: replayBytesSHA256(snapshot.Data), Size: int64(len(snapshot.Data))})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })

	counts := make(map[string]ReviewIndexObject, len(artifacts))
	for _, artifact := range artifacts {
		object := counts[artifact.SHA256]
		object.SHA256 = artifact.SHA256
		object.Size = artifact.Size
		object.ArtifactCount++
		counts[artifact.SHA256] = object
	}
	objects := make([]ReviewIndexObject, 0, len(counts))
	for _, object := range counts {
		objects = append(objects, object)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].SHA256 < objects[j].SHA256 })

	index := ReviewIndex{SchemaVersion: 1, AttemptPath: attemptPath, AttemptSHA256: replayBytesSHA256(attemptBytes), PredecessorPath: predecessorPath, PredecessorSHA256: predecessorSHA, Artifacts: artifacts, Objects: objects}
	index.IndexSHA256 = reviewIndexSHA256(index)
	if err := ValidateReviewIndex(index); err != nil {
		return ReviewIndex{}, err
	}
	if err := WriteNewJSON(request.OutputPath, index); err != nil {
		return ReviewIndex{}, err
	}
	return index, nil
}

func LoadReviewIndex(path string) (ReviewIndex, error) {
	index, _, err := readReviewIndex(path)
	return index, err
}

// VerifyReviewIndex checks both the sealed index and the unchanged bytes at
// every retained path. A review is invalid if any raw evidence was replaced.
func VerifyReviewIndex(path string) (ReviewIndex, error) {
	index, _, err := verifyReviewIndex(path, make(map[string]struct{}))
	return index, err
}

func verifyReviewIndex(path string, seen map[string]struct{}) (ReviewIndex, []byte, error) {
	if err := validateCleanReviewPath(path, "review index"); err != nil {
		return ReviewIndex{}, nil, err
	}
	if _, exists := seen[path]; exists {
		return ReviewIndex{}, nil, fmt.Errorf("review index predecessor cycle: %s", path)
	}
	seen[path] = struct{}{}
	index, indexBytes, err := readReviewIndex(path)
	if err != nil {
		return ReviewIndex{}, nil, err
	}
	attempt, attemptBytes, err := readReviewIndexAttempt(index.AttemptPath)
	if err != nil || ValidateAssuranceAttempt(attempt) != nil || replayBytesSHA256(attemptBytes) != index.AttemptSHA256 {
		return ReviewIndex{}, nil, fmt.Errorf("review index attempt changed: %s", index.AttemptPath)
	}
	if index.PredecessorPath != "" {
		_, predecessorBytes, err := verifyReviewIndex(index.PredecessorPath, seen)
		if err != nil {
			return ReviewIndex{}, nil, fmt.Errorf("verify review index predecessor %s: %w", index.PredecessorPath, err)
		}
		if replayBytesSHA256(predecessorBytes) != index.PredecessorSHA256 {
			return ReviewIndex{}, nil, fmt.Errorf("review index predecessor changed: %s", index.PredecessorPath)
		}
	}
	objects := make(map[string]ReviewIndexObject, len(index.Objects))
	for _, object := range index.Objects {
		objects[object.SHA256] = object
	}
	for _, artifact := range index.Artifacts {
		snapshot, err := readRegularFileSnapshot(artifact.Path)
		if err != nil {
			return ReviewIndex{}, nil, fmt.Errorf("verify review artifact %s: %w", artifact.Path, err)
		}
		if int64(len(snapshot.Data)) != artifact.Size || replayBytesSHA256(snapshot.Data) != artifact.SHA256 {
			return ReviewIndex{}, nil, fmt.Errorf("review artifact changed: %s", artifact.Path)
		}
		object, ok := objects[artifact.SHA256]
		if !ok || object.Size != artifact.Size {
			return ReviewIndex{}, nil, fmt.Errorf("review artifact object is not bound: %s", artifact.Path)
		}
	}
	return index, indexBytes, nil
}

func readReviewIndex(path string) (ReviewIndex, []byte, error) {
	if err := validateCleanReviewPath(path, "review index"); err != nil {
		return ReviewIndex{}, nil, err
	}
	snapshot, err := readRegularFileSnapshot(path)
	if err != nil {
		return ReviewIndex{}, nil, err
	}
	var index ReviewIndex
	if err := decodeExactJSON(snapshot.Data, &index); err != nil {
		return ReviewIndex{}, nil, err
	}
	if err := ValidateReviewIndex(index); err != nil {
		return ReviewIndex{}, nil, err
	}
	return index, snapshot.Data, nil
}

func ValidateReviewIndex(index ReviewIndex) error {
	if index.SchemaVersion != 1 || index.AttemptPath == "" || !filepath.IsAbs(index.AttemptPath) || filepath.Clean(index.AttemptPath) != index.AttemptPath || !sha256Pattern.MatchString(index.AttemptSHA256) || !sha256Pattern.MatchString(index.IndexSHA256) || len(index.Artifacts) == 0 {
		return fmt.Errorf("invalid review index bindings")
	}
	if (index.PredecessorPath == "") != (index.PredecessorSHA256 == "") || (index.PredecessorPath != "" && (!filepath.IsAbs(index.PredecessorPath) || filepath.Clean(index.PredecessorPath) != index.PredecessorPath || !sha256Pattern.MatchString(index.PredecessorSHA256))) {
		return fmt.Errorf("invalid predecessor review index binding")
	}
	if index.IndexSHA256 != reviewIndexSHA256(index) {
		return fmt.Errorf("review index hash does not match contents")
	}
	seenPaths := make(map[string]struct{}, len(index.Artifacts))
	for i, artifact := range index.Artifacts {
		if artifact.Path == "" || !filepath.IsAbs(artifact.Path) || filepath.Clean(artifact.Path) != artifact.Path || !sha256Pattern.MatchString(artifact.SHA256) || artifact.Size < 0 {
			return fmt.Errorf("invalid review artifact at index %d", i)
		}
		if _, exists := seenPaths[artifact.Path]; exists {
			return fmt.Errorf("duplicate review artifact path: %s", artifact.Path)
		}
		seenPaths[artifact.Path] = struct{}{}
		if i > 0 && index.Artifacts[i-1].Path >= artifact.Path {
			return fmt.Errorf("review artifacts are not sorted")
		}
	}
	seenObjects := make(map[string]struct{}, len(index.Objects))
	counts := make(map[string]int, len(index.Objects))
	for i, object := range index.Objects {
		if !sha256Pattern.MatchString(object.SHA256) || object.Size < 0 || object.ArtifactCount < 1 {
			return fmt.Errorf("invalid review object at index %d", i)
		}
		if _, exists := seenObjects[object.SHA256]; exists {
			return fmt.Errorf("duplicate review object: %s", object.SHA256)
		}
		seenObjects[object.SHA256] = struct{}{}
		counts[object.SHA256] = object.ArtifactCount
		if i > 0 && index.Objects[i-1].SHA256 >= object.SHA256 {
			return fmt.Errorf("review objects are not sorted")
		}
	}
	for _, artifact := range index.Artifacts {
		if counts[artifact.SHA256] == 0 {
			return fmt.Errorf("review artifact has no object: %s", artifact.Path)
		}
		counts[artifact.SHA256]--
	}
	for hash, count := range counts {
		if count != 0 {
			return fmt.Errorf("review object count mismatch for %s", hash)
		}
	}
	return nil
}

func reviewIndexSHA256(index ReviewIndex) string {
	digest := reviewIndexDigest{SchemaVersion: index.SchemaVersion, AttemptPath: index.AttemptPath, AttemptSHA256: index.AttemptSHA256, PredecessorPath: index.PredecessorPath, PredecessorSHA256: index.PredecessorSHA256, Artifacts: index.Artifacts, Objects: index.Objects}
	data, _ := json.Marshal(digest)
	return replayBytesSHA256(data)
}

func readReviewIndexAttempt(path string) (AssuranceAttempt, []byte, error) {
	if err := validateCleanReviewPath(path, "attempt"); err != nil {
		return AssuranceAttempt{}, nil, err
	}
	snapshot, err := readRegularFileSnapshot(path)
	if err != nil {
		return AssuranceAttempt{}, nil, err
	}
	var attempt AssuranceAttempt
	if err := decodeExactJSON(snapshot.Data, &attempt); err != nil {
		return AssuranceAttempt{}, nil, err
	}
	return attempt, snapshot.Data, nil
}

func validateCleanReviewPath(path, label string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", label)
	}
	if filepath.Clean(path) != path {
		return fmt.Errorf("%s path must be clean", label)
	}
	return nil
}
