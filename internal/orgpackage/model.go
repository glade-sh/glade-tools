package orgpackage

import (
	"time"

	"github.com/glade-sh/glade/internal/orgdescribe"
)

type Capture struct {
	Package          PackageIdentity
	Org              OrgIdentity
	ApexClasses      []ApexClassContract
	Objects          []orgdescribe.SObject
	Labels           []string
	StaticResources  []string
	LightningBundles []LightningBundleContract
	CapturedAt       time.Time
}

type Options struct {
	TargetOrg  string
	Namespace  string
	APIVersion string
	SFBin      string
}

type PackageIdentity struct {
	Namespace   string
	Name        string
	Version     string
	PackageID   string
	InstalledID string
}

type OrgIdentity struct {
	OrgID      string
	Username   string
	TargetOrg  string
	APIVersion string
}

type ApexClassContract struct {
	Name         string
	Namespace    string
	Visibility   string
	Abstract     bool
	Interface    bool
	Enum         bool
	SuperClass   string
	Interfaces   []string
	Methods      []ApexMethodContract
	Properties   []ApexPropertyContract
	Constructors []ApexMethodContract
}

type ApexMethodContract struct {
	Name       string
	ReturnType string
	Visibility string
	Static     bool
	Abstract   bool
	Parameters []ApexParameterContract
}

type ApexPropertyContract struct {
	Name       string
	Type       string
	Visibility string
	Static     bool
}

type ApexParameterContract struct {
	Name string
	Type string
}

type LightningBundleContract struct {
	Namespace string
	Name      string
	Type      string
	Exposed   bool
}
