package orgpackage

import (
	"errors"
	"sort"
	"strings"

	"github.com/glade-sh/glade/internal/apexast"
	"github.com/glade-sh/glade/internal/orgdescribe"
	"github.com/glade-sh/glade/internal/packageartifact"
)

func Convert(capture Capture) (packageartifact.Artifact, error) {
	namespace := strings.TrimSpace(capture.Package.Namespace)
	if namespace == "" {
		return packageartifact.Artifact{}, errors.New("package namespace is required")
	}
	apexTypes := make([]packageartifact.ApexType, 0, len(capture.ApexClasses))
	for _, row := range capture.ApexClasses {
		if !visibleContract(row.Visibility) {
			continue
		}
		apexTypes = append(apexTypes, apexTypeFromContract(row, capture.Package.Version))
	}
	sort.Slice(apexTypes, func(i, j int) bool { return apexTypes[i].Name < apexTypes[j].Name })
	objects := orgdescribe.Catalog{Objects: capture.Objects}.ToSchema().Objects
	labels := sortedUniqueStrings(capture.Labels)
	resources := sortedUniqueStrings(capture.StaticResources)
	return packageartifact.BuildCaptured(packageartifact.BuildCapturedOptions{
		Namespace:        namespace,
		PackageName:      strings.TrimSpace(capture.Package.Name),
		Version:          strings.TrimSpace(capture.Package.Version),
		SourceAPIVersion: strings.TrimSpace(capture.Org.APIVersion),
		Capture: packageartifact.CaptureProvenance{
			Source:      "org",
			OrgID:       strings.TrimSpace(capture.Org.OrgID),
			Username:    strings.TrimSpace(capture.Org.Username),
			TargetOrg:   strings.TrimSpace(capture.Org.TargetOrg),
			APIVersion:  strings.TrimSpace(capture.Org.APIVersion),
			CapturedAt:  capture.CapturedAt,
			PackageID:   strings.TrimSpace(capture.Package.PackageID),
			InstalledID: strings.TrimSpace(capture.Package.InstalledID),
		},
		ApexTypes:             apexTypes,
		Objects:               objects,
		LabelNames:            labels,
		StaticResourceNames:   resources,
		LightningBundles:      packageLightningBundles(capture.LightningBundles),
		CustomMetadataRecords: nil,
	})
}

func apexTypeFromContract(row ApexClassContract, version string) packageartifact.ApexType {
	kind := apexast.DeclarationClass
	if row.Interface {
		kind = apexast.DeclarationInterface
	}
	if row.Enum {
		kind = apexast.DeclarationEnum
	}
	return packageartifact.ApexType{
		Kind:       kind,
		Name:       strings.TrimSpace(row.Name),
		Namespace:  strings.TrimSpace(row.Namespace),
		Version:    strings.TrimSpace(version),
		Dependency: true,
		Modifiers:  modifierList(row.Visibility, false, row.Abstract),
		SuperClass: strings.TrimSpace(row.SuperClass),
		Interfaces: sortedUniqueStrings(row.Interfaces),
		Members:    apexMembersFromContract(row),
	}
}

func apexMembersFromContract(row ApexClassContract) []packageartifact.ApexMember {
	members := make([]packageartifact.ApexMember, 0, len(row.Constructors)+len(row.Methods)+len(row.Properties))
	for _, method := range row.Constructors {
		if visibleContract(method.Visibility) {
			members = append(members, methodMember(method, apexast.DeclarationConstructor))
		}
	}
	for _, method := range row.Methods {
		if visibleContract(method.Visibility) {
			members = append(members, methodMember(method, apexast.DeclarationMethod))
		}
	}
	for _, property := range row.Properties {
		if visibleContract(property.Visibility) {
			members = append(members, packageartifact.ApexMember{
				Kind:      apexast.DeclarationProperty,
				Name:      strings.TrimSpace(property.Name),
				Type:      strings.TrimSpace(property.Type),
				Modifiers: modifierList(property.Visibility, property.Static, false),
			})
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Name == members[j].Name {
			return members[i].Kind < members[j].Kind
		}
		return members[i].Name < members[j].Name
	})
	return members
}

func methodMember(method ApexMethodContract, kind apexast.DeclarationKind) packageartifact.ApexMember {
	params := make([]apexast.Parameter, 0, len(method.Parameters))
	for _, param := range method.Parameters {
		params = append(params, apexast.Parameter{Name: strings.TrimSpace(param.Name), Type: strings.TrimSpace(param.Type)})
	}
	return packageartifact.ApexMember{
		Kind:       kind,
		Name:       strings.TrimSpace(method.Name),
		Type:       strings.TrimSpace(method.ReturnType),
		Modifiers:  modifierList(method.Visibility, method.Static, method.Abstract),
		Parameters: params,
	}
}

func modifierList(visibility string, isStatic bool, isAbstract bool) []string {
	modifiers := make([]string, 0, 3)
	normalized := strings.ToLower(strings.TrimSpace(visibility))
	if normalized != "" {
		modifiers = append(modifiers, normalized)
	}
	if isStatic {
		modifiers = append(modifiers, "static")
	}
	if isAbstract {
		modifiers = append(modifiers, "abstract")
	}
	return sortedUniqueStrings(modifiers)
}

func visibleContract(visibility string) bool {
	switch strings.ToLower(strings.TrimSpace(visibility)) {
	case "global", "namespaceaccessible":
		return true
	default:
		return false
	}
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	sort.Strings(out)
	return out
}

func sortedLightningBundles(values []LightningBundleContract) []LightningBundleContract {
	out := append([]LightningBundleContract(nil), values...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			if out[i].Namespace == out[j].Namespace {
				return out[i].Name < out[j].Name
			}
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Type < out[j].Type
	})
	return out
}

func packageLightningBundles(values []LightningBundleContract) []packageartifact.LightningBundle {
	values = sortedLightningBundles(values)
	out := make([]packageartifact.LightningBundle, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			continue
		}
		out = append(out, packageartifact.LightningBundle{
			Namespace: strings.TrimSpace(value.Namespace),
			Name:      name,
			Type:      strings.TrimSpace(value.Type),
			Exposed:   value.Exposed,
		})
	}
	return out
}
