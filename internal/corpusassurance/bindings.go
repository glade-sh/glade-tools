package corpusassurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

var reportSnapshotState struct {
	sync.RWMutex
	files map[string][]byte
}

func setReportSnapshot(files map[string][]byte) {
	reportSnapshotState.Lock()
	reportSnapshotState.files = files
	reportSnapshotState.Unlock()
}

func clearReportSnapshot() {
	reportSnapshotState.Lock()
	reportSnapshotState.files = nil
	reportSnapshotState.Unlock()
}

func readAssuranceFile(path string) ([]byte, error) {
	reportSnapshotState.RLock()
	data, ok := reportSnapshotState.files[path]
	if ok {
		data = append([]byte(nil), data...)
	}
	reportSnapshotState.RUnlock()
	if ok {
		return data, nil
	}
	return os.ReadFile(path)
}

type SealedHostInputs struct {
	Inventory InventorySpec
	Root      InventoryManifest
	Host      HostManifest
	Bindings  ReplayBindings
}

func LoadSealedHostInputs(inventoryPath, rootPath, hostPath, expectedHost string) (SealedHostInputs, error) {
	if !filepath.IsAbs(inventoryPath) || !filepath.IsAbs(rootPath) || !filepath.IsAbs(hostPath) || (expectedHost != "local" && expectedHost != "replay-worker") {
		return SealedHostInputs{}, fmt.Errorf("sealed manifest paths and host are required")
	}
	inventory, inventoryBytes, err := readInventorySpec(inventoryPath)
	if err != nil {
		return SealedHostInputs{}, err
	}
	root, rootBytes, err := readExactJSONBytes[InventoryManifest](rootPath)
	if err != nil {
		return SealedHostInputs{}, err
	}
	host, hostBytes, err := readExactJSONBytes[HostManifest](hostPath)
	if err != nil {
		return SealedHostInputs{}, err
	}
	inventorySHA256 := replayBytesSHA256(inventoryBytes)
	rootSHA256 := replayBytesSHA256(rootBytes)
	hostSHA256 := replayBytesSHA256(hostBytes)
	if root.SchemaVersion != 1 || root.InventorySHA256 != inventorySHA256 || ValidateAssuranceAttempt(root.Attempt) != nil || root.Attempt.InventorySHA256 != inventorySHA256 || !sha256Pattern.MatchString(rootSHA256) || host.SchemaVersion != 1 || host.Host != expectedHost || host.RootManifestSHA256 != rootSHA256 {
		return SealedHostInputs{}, fmt.Errorf("sealed manifest bindings do not match")
	}
	if err := ValidateInventoryCoverage(inventory, root.Repositories); err != nil {
		return SealedHostInputs{}, err
	}
	expected := make(map[string]RepositorySpec)
	for _, repository := range root.Repositories {
		if repositoryReplaysOnHost(repository, expectedHost) {
			expected[repository.ID] = repository
		}
	}
	if len(host.Repositories) != len(expected) {
		return SealedHostInputs{}, fmt.Errorf("host manifest repository count mismatch")
	}
	for _, repository := range host.Repositories {
		if expected[repository.ID] != repository {
			return SealedHostInputs{}, fmt.Errorf("host manifest repository %q does not match root", repository.ID)
		}
		delete(expected, repository.ID)
	}
	if len(expected) != 0 {
		return SealedHostInputs{}, fmt.Errorf("host manifest is missing repositories")
	}
	return SealedHostInputs{Inventory: inventory, Root: root, Host: host, Bindings: ReplayBindings{InventorySHA256: inventorySHA256, RootManifestSHA256: rootSHA256, HostManifestSHA256: hostSHA256}}, nil
}

func readExactJSON[T any](path string) (T, error) {
	value, _, err := readExactJSONBytes[T](path)
	return value, err
}

func readExactJSONBytes[T any](path string) (T, []byte, error) {
	var value T
	data, err := readAssuranceFile(path)
	if err != nil {
		return value, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return value, nil, fmt.Errorf("multiple JSON values")
		}
		return value, nil, err
	}
	return value, data, nil
}

func decodeExactJSON(data []byte, value any) error {
	typeOf := reflect.TypeOf(value)
	if typeOf == nil || typeOf.Kind() != reflect.Ptr {
		return fmt.Errorf("exact JSON destination must be a pointer")
	}
	if err := validateExactJSONValue(data, typeOf.Elem()); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateExactJSONValue(data []byte, typeOf reflect.Type) error {
	for typeOf.Kind() == reflect.Ptr {
		typeOf = typeOf.Elem()
	}
	if typeOf.Implements(reflect.TypeFor[json.Unmarshaler]()) || reflect.PointerTo(typeOf).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	switch typeOf.Kind() {
	case reflect.Struct:
		return validateExactJSONObject(data, exactJSONFields(typeOf), nil)
	case reflect.Map:
		if typeOf.Key().Kind() != reflect.String {
			return nil
		}
		return validateExactJSONObject(data, nil, typeOf.Elem())
	case reflect.Array, reflect.Slice:
		return validateExactJSONArray(data, typeOf.Elem())
	case reflect.Interface:
		return validateJSONWithoutDuplicateKeys(data)
	default:
		return nil
	}
}

func validateExactJSONObject(data []byte, fields map[string]reflect.Type, mapValueType reflect.Type) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("expected JSON object")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return fmt.Errorf("duplicate JSON key")
		}
		seen[key] = true
		fieldType := mapValueType
		if fields != nil {
			var found bool
			fieldType, found = fields[key]
			if !found {
				return fmt.Errorf("unexpected JSON key")
			}
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if err := validateExactJSONValue(value, fieldType); err != nil {
			return err
		}
	}
	token, err = decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '}' {
		return fmt.Errorf("expected JSON object terminator")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func validateExactJSONArray(data []byte, elementType reflect.Type) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
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
		if err := validateExactJSONValue(value, elementType); err != nil {
			return err
		}
	}
	token, err = decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != ']' {
		return fmt.Errorf("expected JSON array terminator")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func validateJSONWithoutDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := validateJSONToken(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func validateJSONToken(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate JSON key")
			}
			seen[key] = true
			if err := validateJSONToken(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := validateJSONToken(decoder); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	end, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing, ok := end.(json.Delim); !ok || (delimiter == '{' && closing != '}') || (delimiter == '[' && closing != ']') {
		return fmt.Errorf("invalid JSON delimiter")
	}
	return nil
}

func exactJSONFields(typeOf reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" && field.Anonymous {
			embedded := field.Type
			for embedded.Kind() == reflect.Ptr {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				for key, value := range exactJSONFields(embedded) {
					fields[key] = value
				}
				continue
			}
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}
