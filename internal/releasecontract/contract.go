package releasecontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

type Contract struct {
	SchemaVersion          int        `json:"schemaVersion"`
	Defaults               Defaults   `json:"defaults"`
	Windows                Windows    `json:"windows"`
	Releases               []Release  `json:"releases"`
	Behaviors              []Behavior `json:"behaviors"`
	NoFallbackProductTests []string   `json:"noFallbackProductTests"`
}

type Defaults struct {
	Source     string `json:"source"`
	Endpoint   string `json:"endpoint"`
	OrgProfile string `json:"orgProfile"`
}

type Windows struct {
	Source      []VersionProof `json:"source"`
	Endpoint    []VersionProof `json:"endpoint"`
	OrgProfiles []ProfileProof `json:"orgProfiles"`
}

type VersionProof struct {
	Version      string   `json:"version"`
	ProofCases   []string `json:"proofCases,omitempty"`
	ProductTests []string `json:"productTests,omitempty"`
}

type ProfileProof struct {
	Name         string   `json:"name"`
	ProofCases   []string `json:"proofCases,omitempty"`
	ProductTests []string `json:"productTests,omitempty"`
}

type Release struct {
	Name            string `json:"name"`
	APIVersion      string `json:"apiVersion"`
	Maturity        string `json:"maturity"`
	Manifest        string `json:"manifest"`
	Inventory       string `json:"inventory"`
	Classifications string `json:"classifications,omitempty"`
	ChangeInventory string `json:"changeInventory,omitempty"`
	ChangeRoutes    string `json:"changeRoutes,omitempty"`
}

type Behavior struct {
	ID           string   `json:"id"`
	Axis         string   `json:"axis"`
	Kind         string   `json:"kind"`
	Outcome      string   `json:"outcome"`
	Since        string   `json:"since,omitempty"`
	Until        string   `json:"until,omitempty"`
	Maturity     string   `json:"maturity"`
	SurfaceIDs   []string `json:"surfaceIds,omitempty"`
	Requirements []string `json:"requirements,omitempty"`
	SourceRefs   []string `json:"sourceRefs"`
	ProofCases   []string `json:"proofCases,omitempty"`
	ProductTests []string `json:"productTests,omitempty"`
}

var apiVersionPattern = regexp.MustCompile(`^[1-9][0-9]*[.]0$`)

func Load(path string) (Contract, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Contract{}, "", err
	}
	var contract Contract
	if err := validateExactJSON(data, reflect.TypeOf(contract)); err != nil {
		return Contract{}, "", err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&contract); err != nil {
		return Contract{}, "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Contract{}, "", fmt.Errorf("trailing JSON")
		}
		return Contract{}, "", err
	}
	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Contract{}, "", err
	}
	if err := contract.Validate(root); err != nil {
		return contract, root, err
	}
	return contract, root, nil
}

func (c Contract) Validate(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if c.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1")
	}
	if err := validateAPIVersion("defaults.source", c.Defaults.Source); err != nil {
		return err
	}
	if err := validateAPIVersion("defaults.endpoint", c.Defaults.Endpoint); err != nil {
		return err
	}
	if err := validateSourceWindow(root, c.Windows.Source); err != nil {
		return err
	}
	if err := validateEndpointWindow(root, c.Windows.Endpoint); err != nil {
		return err
	}
	if err := validateProfiles(root, c.Windows.OrgProfiles); err != nil {
		return err
	}
	if !containsVersion(c.Windows.Source, c.Defaults.Source) {
		return fmt.Errorf("default source version %q is outside source window", c.Defaults.Source)
	}
	if !containsVersion(c.Windows.Endpoint, c.Defaults.Endpoint) {
		return fmt.Errorf("default endpoint version %q is outside endpoint window", c.Defaults.Endpoint)
	}
	if !containsProfile(c.Windows.OrgProfiles, c.Defaults.OrgProfile) {
		return fmt.Errorf("default org profile %q is outside org profile window", c.Defaults.OrgProfile)
	}
	if err := validateReleases(root, c.Releases, c.Windows.Source); err != nil {
		return err
	}
	if err := validateBehaviors(root, c.Behaviors, c.Windows.Source, c.Windows.Endpoint); err != nil {
		return err
	}
	if err := validateNoFallbackProductTests(root, c.NoFallbackProductTests); err != nil {
		return err
	}
	return nil
}

func validateSourceWindow(root string, entries []VersionProof) error {
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		if err := validateAPIVersion(fmt.Sprintf("windows.source[%d].version", index), entry.Version); err != nil {
			return err
		}
		if _, ok := seen[entry.Version]; ok {
			return fmt.Errorf("duplicate source window version %q", entry.Version)
		}
		seen[entry.Version] = struct{}{}
		if err := validateProofs(root, fmt.Sprintf("windows.source[%d]", index), entry.ProofCases, entry.ProductTests); err != nil {
			return err
		}
	}
	return nil
}

func validateEndpointWindow(root string, entries []VersionProof) error {
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		if err := validateAPIVersion(fmt.Sprintf("windows.endpoint[%d].version", index), entry.Version); err != nil {
			return err
		}
		if _, ok := seen[entry.Version]; ok {
			return fmt.Errorf("duplicate endpoint window version %q", entry.Version)
		}
		seen[entry.Version] = struct{}{}
		if err := validateProofs(root, fmt.Sprintf("windows.endpoint[%d]", index), entry.ProofCases, entry.ProductTests); err != nil {
			return err
		}
	}
	return nil
}

func validateProfiles(root string, entries []ProfileProof) error {
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" {
			return fmt.Errorf("windows.orgProfiles[%d].name is empty", index)
		}
		if _, ok := seen[entry.Name]; ok {
			return fmt.Errorf("duplicate org profile %q", entry.Name)
		}
		seen[entry.Name] = struct{}{}
		if err := validateProofs(root, fmt.Sprintf("windows.orgProfiles[%d]", index), entry.ProofCases, entry.ProductTests); err != nil {
			return err
		}
	}
	return nil
}

func validateProofs(root, field string, proofCases, productTests []string) error {
	if len(proofCases) == 0 && len(productTests) == 0 {
		return fmt.Errorf("%s must have proofCases or productTests", field)
	}
	for index, proof := range proofCases {
		if strings.TrimSpace(proof) == "" {
			return fmt.Errorf("%s.proofCases[%d] is empty", field, index)
		}
	}
	for index, productTest := range productTests {
		if err := validateProductTest(root, fmt.Sprintf("%s.productTests[%d]", field, index), productTest); err != nil {
			return err
		}
	}
	return nil
}

func validateReleases(root string, releases []Release, sourceWindow []VersionProof) error {
	seen := make(map[string]struct{}, len(releases))
	for index, release := range releases {
		if err := validateAPIVersion(fmt.Sprintf("releases[%d].apiVersion", index), release.APIVersion); err != nil {
			return err
		}
		if !validMaturity(release.Maturity) {
			return fmt.Errorf("releases[%d].maturity %q is invalid", index, release.Maturity)
		}
		if _, ok := seen[release.APIVersion]; ok {
			return fmt.Errorf("duplicate release API version %q", release.APIVersion)
		}
		seen[release.APIVersion] = struct{}{}
		if index > 0 && compareAPIVersions(releases[index-1].APIVersion, release.APIVersion) >= 0 {
			return fmt.Errorf("release API versions must be strictly ascending")
		}
		for _, path := range []struct {
			name  string
			value string
		}{{"manifest", release.Manifest}, {"inventory", release.Inventory}} {
			if err := validateRepositoryPath(root, fmt.Sprintf("releases[%d].%s", index, path.name), path.value); err != nil {
				return err
			}
		}
		if index == 0 {
			if strings.TrimSpace(release.Classifications) != "" || strings.TrimSpace(release.ChangeInventory) != "" || strings.TrimSpace(release.ChangeRoutes) != "" {
				return fmt.Errorf("first release cannot have change metadata")
			}
		} else {
			for _, path := range []struct {
				name  string
				value string
			}{{"classifications", release.Classifications}, {"changeInventory", release.ChangeInventory}, {"changeRoutes", release.ChangeRoutes}} {
				if err := validateRepositoryPath(root, fmt.Sprintf("releases[%d].%s", index, path.name), path.value); err != nil {
					return err
				}
			}
		}
	}
	for _, source := range sourceWindow {
		count := 0
		for _, release := range releases {
			if release.APIVersion != source.Version {
				continue
			}
			if release.Maturity != "ga" {
				return fmt.Errorf("source window version %q requires a GA release", source.Version)
			}
			count++
		}
		if count != 1 {
			return fmt.Errorf("source window version %q must have one GA release snapshot", source.Version)
		}
	}
	return nil
}

func validateBehaviors(root string, behaviors []Behavior, sourceWindow, endpointWindow []VersionProof) error {
	seen := make(map[string]struct{}, len(behaviors))
	for index, behavior := range behaviors {
		if strings.TrimSpace(behavior.ID) == "" {
			return fmt.Errorf("behaviors[%d].id is empty", index)
		}
		if _, ok := seen[behavior.ID]; ok {
			return fmt.Errorf("duplicate behavior ID %q", behavior.ID)
		}
		seen[behavior.ID] = struct{}{}
		if behavior.Axis != "source" && behavior.Axis != "endpoint" && behavior.Axis != "org-capability" {
			return fmt.Errorf("behaviors[%d].axis %q is invalid", index, behavior.Axis)
		}
		switch behavior.Kind {
		case "added", "changed", "removed", "deprecated", "retired", "maturity":
		default:
			return fmt.Errorf("behaviors[%d].kind %q is invalid", index, behavior.Kind)
		}
		if behavior.Outcome != "supported" && behavior.Outcome != "explicit-non-parity" {
			return fmt.Errorf("behaviors[%d].outcome %q is invalid", index, behavior.Outcome)
		}
		if !validMaturity(behavior.Maturity) {
			return fmt.Errorf("behaviors[%d].maturity %q is invalid", index, behavior.Maturity)
		}
		if behavior.Since == "" && behavior.Until == "" {
			return fmt.Errorf("behaviors[%d] must have since or until", index)
		}
		if behavior.Since != "" {
			if err := validateAPIVersion(fmt.Sprintf("behaviors[%d].since", index), behavior.Since); err != nil {
				return err
			}
		}
		if behavior.Until != "" {
			if err := validateAPIVersion(fmt.Sprintf("behaviors[%d].until", index), behavior.Until); err != nil {
				return err
			}
		}
		if behavior.Since != "" && behavior.Until != "" && compareAPIVersions(behavior.Since, behavior.Until) >= 0 {
			return fmt.Errorf("behaviors[%d] since must be before until", index)
		}
		if behavior.Kind == "maturity" {
			switch behavior.Axis {
			case "source":
				if behavior.Since != "" && !containsVersion(sourceWindow, behavior.Since) {
					return fmt.Errorf("behaviors[%d].since %q is outside the advertised source window", index, behavior.Since)
				}
				if behavior.Until != "" && !containsVersion(sourceWindow, behavior.Until) {
					return fmt.Errorf("behaviors[%d].until %q is outside the advertised source window", index, behavior.Until)
				}
			case "endpoint":
				if behavior.Since != "" && !containsVersion(endpointWindow, behavior.Since) {
					return fmt.Errorf("behaviors[%d].since %q is outside the advertised endpoint window", index, behavior.Since)
				}
				if behavior.Until != "" && !containsVersion(endpointWindow, behavior.Until) {
					return fmt.Errorf("behaviors[%d].until %q is outside the advertised endpoint window", index, behavior.Until)
				}
			}
		}
		if len(behavior.SourceRefs) == 0 {
			return fmt.Errorf("behaviors[%d] must have a Salesforce source reference", index)
		}
		for sourceIndex, sourceRef := range behavior.SourceRefs {
			if err := validateSalesforceURL(fmt.Sprintf("behaviors[%d].sourceRefs[%d]", index, sourceIndex), sourceRef); err != nil {
				return err
			}
		}
		if err := validateProofs(root, fmt.Sprintf("behaviors[%d]", index), behavior.ProofCases, behavior.ProductTests); err != nil {
			return err
		}
	}
	return nil
}

func validateNoFallbackProductTests(root string, productTests []string) error {
	if len(productTests) == 0 {
		return fmt.Errorf("noFallbackProductTests must not be empty")
	}
	seen := make(map[string]struct{}, len(productTests))
	for index, productTest := range productTests {
		if _, ok := seen[productTest]; ok {
			return fmt.Errorf("duplicate noFallbackProductTests entry %q", productTest)
		}
		seen[productTest] = struct{}{}
		if err := validateProductTest(root, fmt.Sprintf("noFallbackProductTests[%d]", index), productTest); err != nil {
			return err
		}
	}
	return nil
}

func validateProductTest(root, field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	separator := strings.LastIndexByte(value, ':')
	if separator <= 0 || separator == len(value)-1 || strings.Contains(value[separator+1:], ":") {
		return fmt.Errorf("%s must be relative/path_test.go:TestName", field)
	}
	path, testName := value[:separator], value[separator+1:]
	if strings.Contains(path, ":") || !strings.HasSuffix(path, "_test.go") || !strings.HasPrefix(testName, "Test") || strings.ContainsAny(testName, " \t\r\n") {
		return fmt.Errorf("%s must be relative/path_test.go:TestName", field)
	}
	if err := validateRepositoryPath(root, field, path); err != nil {
		return err
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return fmt.Errorf("%s must be normalized", field)
	}
	return nil
}

func validateRepositoryPath(root, field, value string) error {
	if strings.TrimSpace(value) == "" || strings.Contains(value, "\\") || filepath.IsAbs(value) || filepath.Clean(value) != value {
		return fmt.Errorf("%s must be a non-empty, relative, clean path", field)
	}
	joined := filepath.Join(root, value)
	relative, err := filepath.Rel(root, joined)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("%s escapes repository root", field)
	}
	return nil
}

func validateSalesforceURL(field, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return fmt.Errorf("%s must be a Salesforce URL", field)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "salesforce.com" && !strings.HasSuffix(host, ".salesforce.com") {
		return fmt.Errorf("%s must be hosted at salesforce.com", field)
	}
	return nil
}

func validateExactJSON(data []byte, typeOf reflect.Type) error {
	if typeOf.Kind() == reflect.Ptr {
		typeOf = typeOf.Elem()
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	switch typeOf.Kind() {
	case reflect.Struct:
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
			return fmt.Errorf("expected JSON object")
		}
		fields := exactJSONFields(typeOf)
		seen := make(map[string]struct{}, len(fields))
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("expected JSON object key")
			}
			if _, ok := seen[key]; ok {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			fieldType, ok := fields[key]
			if !ok {
				return fmt.Errorf("unknown JSON key %q", key)
			}
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return err
			}
			if err := validateExactJSON(value, fieldType); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
		if _, err := decoder.Token(); err != io.EOF {
			return fmt.Errorf("multiple JSON values")
		}
	case reflect.Slice, reflect.Array:
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
			return fmt.Errorf("expected JSON array")
		}
		for decoder.More() {
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return err
			}
			if err := validateExactJSON(value, typeOf.Elem()); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
		if _, err := decoder.Token(); err != io.EOF {
			return fmt.Errorf("multiple JSON values")
		}
	}
	return nil
}

func exactJSONFields(typeOf reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, typeOf.NumField())
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func validateAPIVersion(field, value string) error {
	if !apiVersionPattern.MatchString(value) {
		return fmt.Errorf("%s must match N.0", field)
	}
	return nil
}

func validMaturity(value string) bool {
	return value == "preview" || value == "beta" || value == "ga"
}

func containsVersion(entries []VersionProof, version string) bool {
	for _, entry := range entries {
		if entry.Version == version {
			return true
		}
	}
	return false
}

func containsProfile(entries []ProfileProof, name string) bool {
	for _, entry := range entries {
		if entry.Name == name {
			return true
		}
	}
	return false
}

func compareAPIVersions(left, right string) int {
	left = strings.TrimSuffix(left, ".0")
	right = strings.TrimSuffix(right, ".0")
	if len(left) != len(right) {
		if len(left) < len(right) {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}
