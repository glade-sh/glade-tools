package toolcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glade-sh/glade/tools/internal/orgpackage"
)

type orgPackageCaptureOptions struct {
	TargetOrg     string
	Namespace     string
	Output        string
	APIVersion    string
	SFBin         string
	JSON          bool
	ConfigSnippet bool
}

type orgPackageCaptureResult struct {
	Namespace        string `json:"namespace"`
	PackageName      string `json:"packageName,omitempty"`
	Version          string `json:"version,omitempty"`
	APIVersion       string `json:"apiVersion,omitempty"`
	Output           string `json:"output"`
	ApexTypes        int    `json:"apexTypes,omitempty"`
	Objects          int    `json:"objects,omitempty"`
	Labels           int    `json:"labels,omitempty"`
	StaticResources  int    `json:"staticResources,omitempty"`
	LightningBundles int    `json:"lightningBundles,omitempty"`
	ConfigSnippet    string `json:"configSnippet,omitempty"`

	ArtifactJSON []byte `json:"-"`
}

var captureOrgPackage = captureOrgPackageFromSF

func captureOrgPackageFromSF(ctx context.Context, opts orgPackageCaptureOptions) (orgPackageCaptureResult, error) {
	result, err := orgpackage.CaptureInstalledPackage(ctx, orgpackage.Options{
		TargetOrg:  opts.TargetOrg,
		Namespace:  opts.Namespace,
		APIVersion: opts.APIVersion,
		SFBin:      opts.SFBin,
	})
	if err != nil {
		return orgPackageCaptureResult{}, err
	}
	data, err := json.MarshalIndent(result.Artifact, "", "  ")
	if err != nil {
		return orgPackageCaptureResult{}, err
	}
	return orgPackageCaptureResult{
		Namespace:        result.Artifact.Namespace,
		PackageName:      result.Artifact.PackageName,
		Version:          result.Artifact.Version,
		APIVersion:       result.Artifact.SourceAPIVersion,
		Output:           opts.Output,
		ApexTypes:        len(result.Artifact.ApexTypes),
		Objects:          len(result.Artifact.Objects),
		Labels:           result.Artifact.Labels,
		StaticResources:  result.Artifact.StaticResources,
		LightningBundles: len(result.Artifact.LightningBundles),
		ConfigSnippet:    orgPackageConfigSnippet(result.Artifact.Namespace, opts.Output, result.Artifact.Version),
		ArtifactJSON:     data,
	}, nil
}

func RunOrgPackage(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "orgpackage" {
		args = args[1:]
	}
	if len(args) == 0 || isHelpArg(args[0]) {
		printOrgPackageHelp(stdout)
		return 0
	}
	if args[0] == "manifest" {
		if len(args) != 2 || args[1] != "--json" {
			fmt.Fprintln(stderr, "glade-tools: usage: glade-plugin-orgpackage manifest --json")
			return 1
		}
		if err := writeOrgPackageManifest(stdout); err != nil {
			fmt.Fprintf(stderr, "glade-tools: %v\n", err)
			return 1
		}
		return 0
	}
	if args[0] != "capture" {
		fmt.Fprintf(stderr, "glade-tools: %s\n", orgPackageUsage())
		return 1
	}
	if err := runOrgPackageCapture(ctx, args[1:], stdout); err != nil {
		fmt.Fprintf(stderr, "glade-tools: %v\n", err)
		return 1
	}
	return 0
}

func runOrgPackageCapture(ctx context.Context, args []string, w io.Writer) error {
	opts := orgPackageCaptureOptions{SFBin: "sf"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help", "help":
			printOrgPackageCaptureHelp(w)
			return nil
		case "--target-org":
			value, next, err := requireOrgPackageFlagValue(args, i)
			if err != nil {
				return err
			}
			opts.TargetOrg = value
			i = next
		case "--namespace":
			value, next, err := requireOrgPackageFlagValue(args, i)
			if err != nil {
				return err
			}
			opts.Namespace = value
			i = next
		case "--output":
			value, next, err := requireOrgPackageFlagValue(args, i)
			if err != nil {
				return err
			}
			opts.Output = value
			i = next
		case "--api-version":
			value, next, err := requireOrgPackageFlagValue(args, i)
			if err != nil {
				return err
			}
			opts.APIVersion = value
			i = next
		case "--sf-bin":
			value, next, err := requireOrgPackageFlagValue(args, i)
			if err != nil {
				return err
			}
			opts.SFBin = value
			i = next
		case "--json":
			opts.JSON = true
		case "--config-snippet":
			opts.ConfigSnippet = true
		default:
			return fmt.Errorf("unknown orgpackage capture flag %q", args[i])
		}
	}
	if strings.TrimSpace(opts.TargetOrg) == "" {
		return errors.New("orgpackage capture --target-org is required")
	}
	if strings.TrimSpace(opts.Namespace) == "" {
		return errors.New("orgpackage capture --namespace is required")
	}
	if strings.TrimSpace(opts.Output) == "" {
		return errors.New("orgpackage capture --output is required")
	}

	result, err := captureOrgPackage(ctx, opts)
	if err != nil {
		return err
	}
	if len(result.ArtifactJSON) == 0 {
		return errors.New("orgpackage capture produced no artifact")
	}
	if err := writeOrgPackageArtifact(opts.Output, result.ArtifactJSON); err != nil {
		return err
	}
	if result.Output == "" {
		result.Output = opts.Output
	}
	if result.Namespace == "" {
		result.Namespace = opts.Namespace
	}
	if result.ConfigSnippet == "" {
		result.ConfigSnippet = orgPackageConfigSnippet(result.Namespace, result.Output, result.Version)
	}
	if opts.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	if opts.ConfigSnippet {
		fmt.Fprint(w, result.ConfigSnippet)
		return nil
	}
	fmt.Fprintf(w, "orgpackage capture: wrote %s namespace=%s\n", result.Output, result.Namespace)
	return nil
}

func requireOrgPackageFlagValue(args []string, index int) (string, int, error) {
	if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
		return "", index, fmt.Errorf("%s requires a value", args[index])
	}
	return args[index+1], index + 1, nil
}

func writeOrgPackageArtifact(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data = bytes.TrimRight(data, "\n")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func orgPackageConfigSnippet(namespace, artifactPath string, version string) string {
	spec := namespace + ":artifact:" + artifactPath
	if strings.TrimSpace(version) != "" {
		spec += ":" + strings.TrimSpace(version)
	}
	return fmt.Sprintf("project:\n  managedPackageDependencies: [%q]\n", spec)
}

func printOrgPackageHelp(w io.Writer) {
	fmt.Fprintf(w, "%s\n\nCommands:\n  capture    Capture an installed package artifact from a Salesforce org.\n", orgPackageUsage())
}

func printOrgPackageCaptureHelp(w io.Writer) {
	fmt.Fprint(w, strings.TrimSpace(`
Capture an installed package artifact from a Salesforce org.

Usage:
  glade orgpackage capture --target-org <alias> --namespace <ns> --output <path> [--api-version <version>] [--sf-bin <path>] [--json] [--config-snippet]

Flags:
  --target-org <alias>      Salesforce org alias or username.
  --namespace <ns>          Managed package namespace to capture.
  --output <path>           Artifact path to write.
  --api-version <version>   Salesforce API version to use.
  --sf-bin <path>           Salesforce CLI binary. Defaults to sf.
  --json                    Print a JSON summary.
  --config-snippet          Print a glade.yml dependency snippet.
`)+"\n")
}

func orgPackageUsage() string {
	return "usage: glade orgpackage capture --target-org <alias> --namespace <ns> --output <path> [--api-version <version>] [--sf-bin <path>] [--json] [--config-snippet]"
}
