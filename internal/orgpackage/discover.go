package orgpackage

import (
	"context"
	"fmt"
	"strings"
)

func DiscoverPackage(ctx context.Context, client Client, namespace string) (PackageIdentity, error) {
	soql := "SELECT Id, SubscriberPackageId, SubscriberPackage.Name, SubscriberPackage.NamespacePrefix, SubscriberPackageVersionId, SubscriberPackageVersion.MajorVersion, SubscriberPackageVersion.MinorVersion, SubscriberPackageVersion.PatchVersion, SubscriberPackageVersion.BuildNumber FROM InstalledSubscriberPackage WHERE SubscriberPackage.NamespacePrefix = '" + strings.ReplaceAll(namespace, "'", "\\'") + "'"
	var result queryResult[installedPackageRow]
	if err := client.ToolingQuery(ctx, soql, &result); err != nil {
		return PackageIdentity{}, err
	}
	if len(result.Records) == 0 {
		return PackageIdentity{}, fmt.Errorf("installed package namespace %q not found in target org", namespace)
	}
	row := result.Records[0]
	return PackageIdentity{
		Namespace:   row.SubscriberPackage.NamespacePrefix,
		Name:        row.SubscriberPackage.Name,
		Version:     fmt.Sprintf("%d.%d.%d.%d", row.SubscriberPackageVersion.MajorVersion, row.SubscriberPackageVersion.MinorVersion, row.SubscriberPackageVersion.PatchVersion, row.SubscriberPackageVersion.BuildNumber),
		PackageID:   row.SubscriberPackageID,
		InstalledID: row.ID,
	}, nil
}

type installedPackageRow struct {
	ID                         string `json:"Id"`
	SubscriberPackageID        string `json:"SubscriberPackageId"`
	SubscriberPackageVersionID string `json:"SubscriberPackageVersionId"`
	SubscriberPackage          struct {
		Name            string `json:"Name"`
		NamespacePrefix string `json:"NamespacePrefix"`
	} `json:"SubscriberPackage"`
	SubscriberPackageVersion struct {
		MajorVersion int `json:"MajorVersion"`
		MinorVersion int `json:"MinorVersion"`
		PatchVersion int `json:"PatchVersion"`
		BuildNumber  int `json:"BuildNumber"`
	} `json:"SubscriberPackageVersion"`
}
