package main

import (
	"context"
	"os"

	"github.com/glade-sh/glade/tools/internal/toolcli"
)

func main() {
	os.Exit(toolcli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
