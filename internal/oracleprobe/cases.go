package oracleprobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var canonicalSurfaceID = regexp.MustCompile(`^apex:[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*(?:\([A-Za-z0-9_.$<>,?\[\]]*\))?)*$`)

func LoadCases(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		return nil, fmt.Errorf("decode oracle case manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode oracle case manifest: trailing JSON value")
	}
	if err := ValidateCases(cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func ValidateCases(cases []Case) error {
	if len(cases) == 0 {
		return fmt.Errorf("oracle case manifest must contain a nonempty array")
	}
	seenCaseIDs := make(map[string]struct{}, len(cases))
	for index, tc := range cases {
		caseID := strings.TrimSpace(tc.ID)
		if caseID == "" || caseID != tc.ID {
			return fmt.Errorf("case %d has a nonempty case ID requirement", index)
		}
		if _, exists := seenCaseIDs[tc.ID]; exists {
			return fmt.Errorf("duplicate case ID %q", tc.ID)
		}
		seenCaseIDs[tc.ID] = struct{}{}
		if tc.Mode != ModeAnonymous {
			return fmt.Errorf("case %q must use anonymous mode", tc.ID)
		}
		if area := strings.TrimSpace(tc.Area); area == "" || area != tc.Area {
			return fmt.Errorf("case %q must have a nonempty area", tc.ID)
		}
		if api := strings.TrimSpace(tc.API); api == "" || api != tc.API {
			return fmt.Errorf("case %q must have a nonempty API", tc.ID)
		}
		if strings.TrimSpace(tc.Expression) == "" {
			return fmt.Errorf("case %q has an empty expression", tc.ID)
		}
		if len(tc.SurfaceIDs) == 0 {
			return fmt.Errorf("case %q must have a nonempty surface ID list", tc.ID)
		}
		seenSurfaceIDs := make(map[string]struct{}, len(tc.SurfaceIDs))
		for _, surfaceID := range tc.SurfaceIDs {
			if !canonicalSurfaceID.MatchString(surfaceID) {
				return fmt.Errorf("case %q has non-canonical surface ID %q", tc.ID, surfaceID)
			}
			if _, exists := seenSurfaceIDs[surfaceID]; exists {
				return fmt.Errorf("case %q has duplicate surface ID %q", tc.ID, surfaceID)
			}
			seenSurfaceIDs[surfaceID] = struct{}{}
		}
	}
	return nil
}
