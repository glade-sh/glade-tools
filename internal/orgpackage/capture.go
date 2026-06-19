package orgpackage

import (
	"context"
	"strings"
	"time"

	"github.com/glade-sh/glade/internal/packageartifact"
)

type Result struct {
	Capture  Capture
	Artifact packageartifact.Artifact
	Summary  Summary
	Warnings []string
}

func CaptureInstalledPackage(ctx context.Context, opts Options) (Result, error) {
	client := Client{
		Runner:     ExecSFRunner{Bin: opts.SFBin},
		TargetOrg:  strings.TrimSpace(opts.TargetOrg),
		APIVersion: strings.TrimSpace(opts.APIVersion),
	}
	namespace := strings.TrimSpace(opts.Namespace)
	pkg, err := DiscoverPackage(ctx, client, namespace)
	if err != nil {
		return Result{}, err
	}
	org, warnings, err := CaptureOrgIdentity(ctx, client)
	if err != nil {
		return Result{}, err
	}
	org.TargetOrg = client.TargetOrg
	if org.APIVersion == "" {
		org.APIVersion = client.apiVersion()
	}
	apexClasses, err := CaptureApexClasses(ctx, client, namespace)
	if err != nil {
		return Result{}, err
	}
	objects, err := CaptureObjects(ctx, client, namespace)
	if err != nil {
		return Result{}, err
	}
	labels, resources, metadataWarnings, err := CaptureMetadataNames(ctx, client, namespace)
	if err != nil {
		return Result{}, err
	}
	warnings = append(warnings, metadataWarnings...)
	bundles, bundleWarnings, err := CaptureLightningBundles(ctx, client, namespace)
	if err != nil {
		return Result{}, err
	}
	warnings = append(warnings, bundleWarnings...)
	capture := Capture{
		Package:          pkg,
		Org:              org,
		ApexClasses:      apexClasses,
		Objects:          objects,
		Labels:           labels,
		StaticResources:  resources,
		LightningBundles: bundles,
		CapturedAt:       time.Now().UTC(),
	}
	artifact, err := Convert(capture)
	if err != nil {
		return Result{}, err
	}
	summary := Summarize(capture, warnings)
	return Result{Capture: capture, Artifact: artifact, Summary: summary, Warnings: warnings}, nil
}

func CaptureOrgIdentity(ctx context.Context, client Client) (OrgIdentity, []string, error) {
	var warnings []string
	var orgResult queryResult[struct {
		ID string `json:"Id"`
	}]
	if err := client.DataQuery(ctx, "SELECT Id FROM Organization", &orgResult); err != nil {
		return OrgIdentity{}, nil, err
	}
	identity := OrgIdentity{APIVersion: client.apiVersion()}
	if len(orgResult.Records) > 0 {
		identity.OrgID = orgResult.Records[0].ID
	}
	var userInfo struct {
		Username string `json:"username"`
	}
	if err := client.Get(ctx, "/services/oauth2/userinfo", &userInfo); err != nil {
		warnings = append(warnings, "could not capture current username: "+err.Error())
		return identity, warnings, nil
	}
	identity.Username = userInfo.Username
	return identity, warnings, nil
}
