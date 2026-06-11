package main

import (
	"context"
	"os"

	"github.com/glade-sh/glade/tools/internal/perftool"
)

func main() {
	os.Exit(perftool.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
