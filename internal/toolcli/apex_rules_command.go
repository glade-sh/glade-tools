package toolcli

import (
	"fmt"
	"io"

	"github.com/glade-sh/glade/tools/internal/apexrules"
)

func runApexRules(args []string, w io.Writer) error {
	if len(args) != 3 || args[0] != "validate" || args[1] != "--catalog" {
		return fmt.Errorf("usage: glade-tools apex-rules validate --catalog <path>")
	}
	if _, err := apexrules.LoadCatalog(args[2]); err != nil {
		return err
	}
	fmt.Fprintln(w, "ok")
	return nil
}
