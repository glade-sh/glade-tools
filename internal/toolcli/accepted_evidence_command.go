package toolcli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/glade-sh/glade/tools/internal/surfaceledger"
)

func runAcceptedEvidence(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var mapPaths []string
	var supportProfilePath string
	var outPath string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--map":
			i++
			if i >= len(args) {
				return fmt.Errorf("--map requires a path")
			}
			if _, err := os.Stat(args[i]); err != nil {
				return fmt.Errorf("map file not found: %s", args[i])
			}
			mapPaths = append(mapPaths, args[i])
		case "--support-profile":
			i++
			if i >= len(args) {
				return fmt.Errorf("--support-profile requires a path")
			}
			if _, err := os.Stat(args[i]); err != nil {
				return fmt.Errorf("support profile file not found: %s", args[i])
			}
			supportProfilePath = args[i]
		case "--out":
			i++
			if i >= len(args) {
				return fmt.Errorf("--out requires a path")
			}
			outPath = args[i]
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	if len(mapPaths) == 0 {
		return fmt.Errorf("at least one --map <path> is required")
	}
	if outPath == "" {
		return fmt.Errorf("--out <path> is required")
	}

	manifest, err := surfaceledger.IngestAcceptedEvidence(mapPaths, supportProfilePath)
	if err != nil {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer f.Close()

	if err := surfaceledger.WriteAcceptedEvidenceJSON(f, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	fmt.Fprintf(stderr, "accepted-evidence manifest written to %s (%d accepted / %d rejected / %d total)\n",
		outPath, manifest.Accepted, manifest.Rejected, manifest.TotalInput)
	return nil
}
